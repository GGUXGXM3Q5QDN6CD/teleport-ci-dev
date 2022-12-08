package main

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/gravitational/teleport/build.assets/tooling/lib/github"
)

func main() {
	args := parseCommandLine()
	ctx := context.TODO()

	gh := github.NewGitHubWitToken(ctx, args.token)

	dispatchCtx, cancelDispatch := context.WithTimeout(ctx, 1*time.Minute)
	defer cancelDispatch()

	runID, err := gh.TriggerDispatchEvent(dispatchCtx, args.owner, args.repo, args.path, args.workflowRef, map[string]interface{}{
		"oss-teleport-ref": args.teleportRef,
		"upload-artifacts": strconv.FormatBool(args.stageArtifacts),
	})
	if err != nil {
		log.Fatalf("Failed to start workflow run %s", err)
	}

	log.Printf("Waiting for workflow run %d to complete", runID)
	conclusion, err := gh.WaitForRun(ctx, args.owner, args.repo, args.path, args.workflowRef, runID)
	if err != nil {
		log.Fatalf("Failed to waiting for run to exit %s", err)
	}

	if conclusion != "success" {
		log.Fatalf("Build failed: %s", conclusion)
	}

	log.Printf("Build succeeded")
}
