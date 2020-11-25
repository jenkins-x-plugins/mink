# jx mink

[![Documentation](https://godoc.org/github.com/jenkins-x-plugins/jx-mink?status.svg)](https://pkg.go.dev/mod/github.com/jenkins-x-plugins/jx-mink)
[![Go Report Card](https://goreportcard.com/badge/github.com/jenkins-x-plugins/jx-mink)](https://goreportcard.com/report/github.com/jenkins-x-plugins/jx-mink)
[![Releases](https://img.shields.io/github/release-pre/jenkins-x/helmboot.svg)](https://github.com/jenkins-x-plugins/jx-mink/releases)
[![LICENSE](https://img.shields.io/github/license/jenkins-x/helmboot.svg)](https://github.com/jenkins-x-plugins/jx-mink/blob/master/LICENSE)
[![Slack Status](https://img.shields.io/badge/slack-join_chat-white.svg?logo=slack&style=social)](https://slack.k8s.io/)

`jx-mink` is a simple command line tool for using [mink](https://github.com/mattmoor/mink) with [Jenkins X](https://jenkins-x.io/) Pipelines to perform image builds and resolve image references in helm charts.


## Getting Started

Download the [jx-mink binary](https://github.com/jenkins-x-plugins/mink/releases) for your operating system and add it to your `$PATH`.

## Commands

See the [jx-mink command reference](https://github.com/jenkins-x-plugins/mink/blob/master/docs/cmd/jx-mink.md#jx-mink)


## Using inside pipelines

The `jx-mink step` command is a plugin replacement for the kaniko images we've been using in [Jenkins X V3](https://jenkins-x.io/v3/) up to now. Its used as follows:

```yaml 
- image: gcr.io/jenkinsxio/jx-mink:0.19.8
  name: build-container-build
  script: |
    #!/busybox/sh
    source .jx/variables.sh
    jx-mink step
```

This will use the `jx-mink init` command to create a `.mink.yaml` file if the file does not already exist and it can find a Dockerfile/build pack and a chart.

You can configure the `.mink.yaml` to point at whatever dockerfiles/buildpacks/charts you want in the usual [mink way](https://github.com/mattmoor/mink).

The step will then invoke the build steps using either kaniko, ko or a build pack to generate the container image(s).

Finally the image digests will be added into any configured YAML file such as the charts `values.yaml` file as an entry `image.fullName` in the released chart.

 
### Using vanilla kaniko

If you wish to switch to using just kaniko without using a `.mink.yaml` file and only creating a single image from a single `Dockerfile` then switch to using `jx-mink build`:

```yaml 
- image: gcr.io/jenkinsxio/jx-mink:0.19.8
  name: build-container-build
  script: |
    #!/busybox/sh
    source .jx/variables.sh
    jx-mink build
```

### Configuration

All of the configuration options you can see via `jx mink step --help`  or `jx mink build --help` are available to be configured inside your pipeline. For any command line argument if you convert it to upper case, replace "-" separators with "_" and add the `"MINK_"` prefix.

e.g. to define the image to build you can specify `MINK_IMAGE` as an environment variable.

To specify environment variables you can modify the pipeilne YAML directly, or you can just add whatever variables you want (with default bash expressions and bash conditions etc) to the file `.jx/variables.sh`.

```bash 
# contents of .jx/variables.sh
                                
# lets disable invoking knaiko locally so that we invoke it with a separate TaskRun
export MINK_LOCAL_KANIKO="false"                                                   

# lets configure the kaniko image we want to use
export MINK_KANIKO_IMAGE="gcr.io/kaniko-project/executor:v1.3.0"                                                   
```

## Using locally

You can use the `jx` CLI to invoke `jx mink` locally to perform image builds on your local laptop. The actual kaniko, ko or build pack builds are performed inside the kubernetes cluster using a `TaskRun`

When using `jx mink step`  or `jx mink build` you can specify the `bundle` parameter if you wish to get mink to create a self extracting image of your local source code. Otherwise `jx mink` defaults to passing the git URL and SHA to the build steps so it can use a regular git clone to get the source code.



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
