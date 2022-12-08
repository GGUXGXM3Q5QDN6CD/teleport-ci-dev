package github

import (
	"context"
	"log"
	"time"

	"github.com/google/go-github/v41/github"
	"github.com/gravitational/trace"
)

func setsAreEqual(a, b map[int64]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, present := b[k]; !present {
			return false
		}
	}
	return true
}

func firstUniqueInSet(newSet, baseSet map[int64]struct{}) (int64, bool) {
	for k, _ := range newSet {
		if _, present := baseSet[k]; !present {
			return k, true
		}
	}
	return 0, false
}

// ListJobs returns a set of all RunIDs for runs create since the supplied start time.
func (gh *ghClient) ListJobs(ctx context.Context, owner, repo, path, ref string, since time.Time) (map[int64]struct{}, error) {
	listOptions := github.ListWorkflowRunsOptions{
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
		Branch:  ref,
		Created: ">" + since.Format(time.RFC3339),
	}

	runIDs := make(map[int64]struct{})

	for {
		runs, resp, err := gh.client.Actions.ListWorkflowRunsByFileName(ctx, owner, repo, path, &listOptions)
		if err != nil {
			return nil, trace.Wrap(err, "Failed to fetch runs")
		}

		for _, r := range runs.WorkflowRuns {
			runIDs[r.GetID()] = struct{}{}
		}

		if resp.NextPage == 0 {
			break
		}

		listOptions.Page = resp.NextPage
	}

	return runIDs, nil
}

func (gh *ghClient) TriggerDispatchEvent(ctx context.Context, owner, repo, path, ref string, inputs map[string]interface{}) (int64, error) {
	baselineTime := time.Now().Add(-2 * time.Minute)
	oldRuns, err := gh.ListJobs(ctx, owner, repo, path, ref, baselineTime)
	if err != nil {
		return 0, trace.Wrap(err, "Failed to fetch task list")
	}

	log.Printf("Attempting to trigger %s/%s %s at ref %s\n", owner, repo, path, ref)
	triggerArgs := github.CreateWorkflowDispatchEventRequest{
		Ref:    ref,
		Inputs: inputs,
	}

	_, err = gh.client.Actions.CreateWorkflowDispatchEventByFileName(ctx, owner, repo, path, triggerArgs)
	if err != nil {
		return 0, trace.Wrap(err, "Failed to issue request")
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	newRuns := make(map[int64]struct{})
	for k, _ := range oldRuns {
		newRuns[k] = struct{}{}
	}

	log.Printf("Waiting for new workflow run to start")

	for setsAreEqual(newRuns, oldRuns) {
		select {
		case <-ticker.C:
			newRuns, err = gh.ListJobs(ctx, owner, repo, path, ref, baselineTime)
			if err != nil {
				return 0, trace.Wrap(err, "Failed to fetch task list")
			}

		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}

	runID, ok := firstUniqueInSet(newRuns, oldRuns)
	if !ok {
		return 0, trace.Errorf("Unable to detect new run ID")
	}

	log.Printf("Started workflow run ID %d", runID)

	run, _, err := gh.client.Actions.GetWorkflowRunByID(ctx, owner, repo, runID)
	if err != nil {
		return 0, trace.Wrap(err, "Failed polling run")
	}

	log.Printf("See: %s", run.GetHTMLURL())

	return runID, nil
}

func (gh *ghClient) WaitForRun(ctx context.Context, owner, repo, path, ref string, runID int64) (string, error) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			run, _, err := gh.client.Actions.GetWorkflowRunByID(ctx, owner, repo, runID)
			if err != nil {
				return "", trace.Wrap(err, "Failed polling run")
			}

			log.Printf("Workflow status: %s", run.GetStatus())

			if run.GetStatus() == "completed" {
				return run.GetConclusion(), nil
			}

		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
