package main

import "flag"

type args struct {
	token          string
	owner          string
	repo           string
	path           string
	workflowRef    string
	teleportRef    string
	stageArtifacts bool
}

func parseCommandLine() args {

	args := args{
		workflowRef: "main",
	}

	flag.StringVar(&args.token, "token", "", "GitHub PAT")
	flag.StringVar(&args.owner, "owner", "", "Owner of the repo to target")
	flag.StringVar(&args.repo, "repo", "", "Repo to target")
	flag.StringVar(&args.path, "path", "", "Path to workflow")
	flag.StringVar(&args.workflowRef, "workflow-ref", args.workflowRef, "Revision reference")
	flag.StringVar(&args.teleportRef, "teleport-ref", args.teleportRef, "OSS Teleport ref to build")
	flag.BoolVar(&args.stageArtifacts, "stage", false, "Upload arrtifacts to staging bucket")

	flag.Parse()

	return args
}
