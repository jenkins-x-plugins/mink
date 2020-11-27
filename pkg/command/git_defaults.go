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

var detectGitURLAndRevision = &gitURLandRevisionResolver{}

// setViperGitAndKanikoDefaults sets up the git and kaniko defaults to be used if no explicit bundle or configuraiton is specified
func setViperGitAndKanikoDefaults(out io.Writer) {
	detectGitURLAndRevision.Out = out

	viper.SetDefault("git-url", detectGitURLAndRevision.GitURL())
	viper.SetDefault("git-rev", detectGitURLAndRevision.GitRevision())

	if kube.IsInCluster() {
		viper.SetDefault("local-kaniko", "true")
	}

	kanikoFlags := os.Getenv("KANIKO_FLAGS")
	if kanikoFlags != "" {
		viper.SetDefault("kaniko-flag", strings.Split(kanikoFlags, " "))
	}
}

type gitURLandRevisionResolver struct {
	Out       io.Writer
	URL       string
	Rev       string
	detectGit bool
}

func (r *gitURLandRevisionResolver) GitURL() string {
	r.resolveHandleError()
	return r.URL
}

func (r *gitURLandRevisionResolver) GitRevision() string {
	r.resolveHandleError()
	return r.Rev
}

func (r *gitURLandRevisionResolver) resolveHandleError() {
	err := r.resolve()
	if err != nil {
		if r.Out == nil {
			r.Out = os.Stderr
		}
		fmt.Fprintf(r.Out, "failed to detect the git URL and revision: %s\n", err.Error())
	}
}

func (g *gitURLandRevisionResolver) resolve() error {
	if g.URL == "" {
		g.URL = os.Getenv("REPO_URL")
	}
	if g.Rev == "" {
		g.Rev = os.Getenv("PULL_PULL_SHA")
		if g.Rev == "" {
			g.Rev = os.Getenv("PULL_BASE_SHA")
		}
	}
	if (g.URL != "" && g.Rev != "") || g.detectGit {
		return nil
	}
	g.detectGit = true
	dir, err := os.Getwd()
	if err != nil {
		return errors.Wrapf(err, "failed to get current directory")
	}

	// lets try default the git URL from git
	r, err := git.PlainOpen(dir)
	if err != nil {
		return errors.Wrapf(err, "failed to open git dir %s", dir)
	}

	if g.URL == "" {
		cfg, err := r.Config()
		if err != nil {
			return errors.Wrapf(err, "failed to get git config")
		}
		if cfg.Remotes == nil {
			return nil
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
		for _, u := range remote.URLs {
			if u != "" {
				// lets remove any local user/passwords if present
				g.URL = stringhelpers.SanitizeURL(u)
				break
			}
		}
	}
	if g.Rev == "" {
		v, err := r.Head()
		if err != nil {
			return errors.Wrapf(err, "failed to find head reference")
		}
		if v != nil {
			g.Rev = v.Hash().String()
		}
	}
	return nil
}
