package command

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/jenkins-x/jx-helpers/v3/pkg/kube"
	"github.com/jenkins-x/jx-helpers/v3/pkg/stringhelpers"
	"github.com/pkg/errors"
	"github.com/spf13/viper"
)

var detectGitAndKaniko = false

// setViperGitAndKanikoDefaults sets up the git and kaniko defaults to be used if no explicit bundle or configuraiton is specified
func setViperGitAndKanikoDefaults(out io.Writer) {
	if detectGitAndKaniko {
		return
	}
	detectGitAndKaniko = true

	gitURL := os.Getenv("REPO_URL")
	gitRev := os.Getenv("PULL_PULL_SHA")
	if gitRev == "" {
		gitRev = os.Getenv("PULL_BASE_SHA")
	}
	if gitURL == "" {
		var err error
		gitURL, gitRev, err = defaultGitURLAndRevision(gitRev)
		if err != nil {
			fmt.Fprintf(out, "failed to detect the git URL: %s\n", err.Error())
		}
		fmt.Fprintf(out, "detected git URL %s and ref %s\n", gitURL, gitRev)
	}

	if gitURL != "" {
		viper.SetDefault("git-url", gitURL)
	}
	if gitRev != "" {
		viper.SetDefault("git-rev", gitRev)
	}
	if kube.IsInCluster() {
		viper.SetDefault("local-kaniko", "true")
	}

	kanikoFlags := os.Getenv("KANIKO_FLAGS")
	if kanikoFlags != "" {
		viper.SetDefault("kaniko-flag", strings.Split(kanikoFlags, " "))
	}
}

func defaultGitURLAndRevision(ref string) (string, string, error) {
	u := ""
	dir, err := os.Getwd()
	if err != nil {
		return u, ref, errors.Wrapf(err, "failed to get current directory")
	}

	// lets try default the git URL from git
	r, err := git.PlainOpen(dir)
	if err != nil {
		return u, ref, errors.Wrapf(err, "failed to open git dir %s", dir)
	}

	cfg, err := r.Config()
	if err != nil {
		return u, ref, errors.Wrapf(err, "failed to get git config")
	}
	if cfg.Remotes == nil {
		return u, ref, nil
	}
	remote := cfg.Remotes["origin"]
	if remote == nil {
		remote = cfg.Remotes["upstream"]
		if remote == nil {
			for _, v := range cfg.Remotes {
				remote = v
				break
			}
		}
	}
	if len(remote.URLs) > 0 {
		u = remote.URLs[0]
		if u != "" {
			// lets remove any local user/passwords if present
			u = stringhelpers.SanitizeURL(u)
		}
	}

	v, err := r.Head()
	if err != nil {
		return u, ref, errors.Wrapf(err, "failed to find head reference")
	}
	if v != nil {
		ref = v.Hash().String()
	}
	return u, ref, nil
}
