// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"
	"log"
	"strings"

	"istio.io/test-infra/sisyphus"
	u "istio.io/test-infra/toolbox/util"
)

const (
	// Alert settings
	sender         = "istio.testing@gmail.com"
	oncallMaillist = "istio-oncall@googlegroups.com"
	subject        = "ATTENTION - Istio Post-Submit Test Failed"
	prologue       = "Hi istio-oncall,\n\n" +
		"Post-Submit is failing in istio/istio, please take a look at following failure(s) and fix ASAP\n\n"
	epilogue = "\nIf you have any questions about this message or notice inaccuracy, please contact istio-engprod@google.com."
	identity = "istio-bot"

	// Prow
	prowProject   = "istio-testing"
	prowZone      = "us-west1-a"
	gubernatorURL = "https://k8s-gubernator.appspot.com/build/istio-prow"
	gcsBucket     = "istio-prow"

	// Branch protection
	owner           = "istio-releases"
	protectedRepo   = "daily-release"
	protectedBranch = "master"
)

var (
	tokenFile            = flag.String("github_token", "/etc/github/git-token", "Path to github token")
	gmailAppPassFile     = flag.String("gmail_app_password", "/etc/gmail/gmail-app-pass", "Path to gmail application password")
	guardProtectedBranch = flag.Bool("guard", false, "Suspend merge bot if postsubmit fails")
	emailSending         = flag.Bool("email_sending", false, "Sending alert email")
	catchFlakesByRun     = flag.Bool("catch_flakes_by_rerun", true, "whether to rerun failed jobs to detect flakyness")
)

func init() {
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	// Connect to the Prow cluster
	if _, err := u.Shell(`gcloud container clusters get-credentials prow \
		--project=%s --zone=%s`, prowProject, prowZone); err != nil {
		log.Fatalf("Unable to switch to prow cluster: %v\n", err)
	}
}

func getProtectedJobs() ([]string, error) {
	var prowTests []string
	prowPrefix := "prow/"
	shSuffix := ".sh"
	ghClnt := u.NewGithubClientNoAuth(owner)
	checks, err := ghClnt.GetLatestChecks(protectedRepo)
	if err != nil {
		return prowTests, err
	}
	for _, check := range checks {
		if strings.HasPrefix(check, prowPrefix) {
			check = strings.TrimPrefix(check, prowPrefix)
			check = strings.TrimSuffix(check, shSuffix)
			prowTests = append(prowTests, check)
		}
	}
	return prowTests, nil
}

func main() {
	prowTests, err := getProtectedJobs()
	if err != nil {
		log.Fatalf("Failed to get the list of prow jobs: %v", err)
	}
	presubmitJobs := prowTests
	gcsClient := u.NewGCSClient(gcsBucket)
	sisyphusd := sisyphus.NewDaemonUsingProw(
		prowTests, presubmitJobs, prowProject, prowZone, gubernatorURL,
		gcsBucket,
		gcsClient,
		sisyphus.NewStorage(),
		&sisyphus.Config{
			CatchFlakesByRun: *catchFlakesByRun,
		})
	if *emailSending {
		gmailAppPass, err := u.GetPasswordFromFile(*gmailAppPassFile)
		if err != nil {
			log.Fatalf("Error accessing gmail app password: %v", err)
		}
		if err := sisyphusd.SetAlert(gmailAppPass, identity, sender, oncallMaillist,
			&sisyphus.AlertConfig{
				Subject:  subject,
				Prologue: prologue,
				Epilogue: epilogue,
			}); err != nil {
			log.Fatalf("Failed to set up alerts: %v", err)
		}
	}
	if *guardProtectedBranch {
		token, err := u.GetAPITokenFromFile(*tokenFile)
		if err != nil {
			log.Fatalf("Error accessing user supplied token_file: %v\n", err)
		}
		sisyphusd.SetProtectedBranch(owner, token, protectedRepo, protectedBranch)
	}
	sisyphusd.Start(context.Background())
}
