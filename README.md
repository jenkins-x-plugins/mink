# jx mink

[![Documentation](https://godoc.org/github.com/jenkins-x-plugins/jx-mink?status.svg)](https://pkg.go.dev/mod/github.com/jenkins-x-plugins/jx-mink)
[![Go Report Card](https://goreportcard.com/badge/github.com/jenkins-x-plugins/jx-mink)](https://goreportcard.com/report/github.com/jenkins-x-plugins/jx-mink)
[![Releases](https://img.shields.io/github/release-pre/jenkins-x/helmboot.svg)](https://github.com/jenkins-x-plugins/jx-mink/releases)
[![LICENSE](https://img.shields.io/github/license/jenkins-x/helmboot.svg)](https://github.com/jenkins-x-plugins/jx-mink/blob/master/LICENSE)
[![Slack Status](https://img.shields.io/badge/slack-join_chat-white.svg?logo=slack&style=social)](https://slack.k8s.io/)

`jx-mink` is a simple command line tool for using [mink](https://github.com/mattmoor/mink) with [Jenkins X](https://jenkins-x.io/) Pipelines to perform image builds and resolve image references in helm charts.


## Getting Started

Download the [jx-mink binary](https://github.com/jenkins-x-plugins/jx-mink/releases) for your operating system and add it to your `$PATH`.

## Commands

See the [jx-mink command reference](https://github.com/jenkins-x-plugins/jx-mink/blob/master/docs/cmd/jx-mink.md#jx-mink)

## Differences from mink

This binary has tried to keep as close to [mink](https://github.com/mattmoor/mink) as possible in code and UX but it has a few minor differences to smooth the integration into [Jenkins X](https://jenkins-x.io/) pipelines. Hopefully over time these differences can combine into a single mink codebase and binary.

We found the easiest way to implement `jx-mink` was via a fork of `mink` but hopefully someday we can align with upstream `mink` and this repository can become just the integration for Jenkins X.
 
### Jenkins X integration 

From a [Jenkins X](https://jenkins-x.io/) users perspective:

* can be invoked via `jx mink` from the Jenkins X command line so that Jenkins X users don't have to install anything
* can be used inside Jenkins X tekton pipelines (for [version 3.x](https://jenkins-x.io/v3/)) without users needing to modify anything
* uses the Jenkins X CI/CD to release binaries

### Mink code differences

This repository includes a few Pull Requests on mink ([#280](https://github.com/mattmoor/mink/pull/280), [#281](https://github.com/mattmoor/mink/pull/281), [#282](https://github.com/mattmoor/mink/pull/282))

It also adds:

* allows local kaniko invocation (so we can avoid an extra chained `TaskRun` by default in Jenkins X pipelines when running `kaniko` pipelines)
* add a `init` command to create ` .mink.yam` file if one is not configured
* adds a `step` command which runs the `init` step first then `resolve` and terminates gracefully if there is no `.mink.yaml` that is defined or can be detected 
