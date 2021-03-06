/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dockerfile

import (
	"context"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	tknv1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/ptr"
)

const (
	// KanikoImage is the path to the kaniko image we use for Dockerfile builds.
	KanikoImage = "gcr.io/kaniko-project/executor:multi-arch"

	// DigestFile default digest file name
	DigestFile = "/tekton/results/IMAGE-DIGEST"
)

// Options holds configuration options specific to Dockerfile builds
type Options struct {
	// Dockerfile is the path to the Dockerfile within the build context.
	Dockerfile string

	// The path within the build context in which to execute the build.
	Path string

	// DigestFile the digest file thats created
	DigestFile string

	// KanikoImage the container image to use for kaniko
	KanikoImage string

	// The extra kaniko arguments for handling things like insecure registries
	KanikoArgs []string
}

// Build returns a TaskRun suitable for performing a Dockerfile build over the
// provided kontext and publishing to the target tag.
func Build(ctx context.Context, sourceSteps []tknv1beta1.Step, target name.Tag, opts Options) *tknv1beta1.TaskRun {
	image := opts.KanikoImage
	if image == "" {
		image = KanikoImage
	}
	digestFile := opts.DigestFile
	if digestFile == "" {
		digestFile = DigestFile
	}
	return &tknv1beta1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dockerfile-",
		},
		Spec: tknv1beta1.TaskRunSpec{
			PodTemplate: &tknv1beta1.PodTemplate{
				EnableServiceLinks: ptr.Bool(false),
			},

			TaskSpec: &tknv1beta1.TaskSpec{
				Results: []tknv1beta1.TaskResult{{
					Name: "IMAGE-DIGEST",
				}},

				Steps: append(sourceSteps, tknv1beta1.Step{
					Container: corev1.Container{
						Name:  "build-and-push",
						Image: image,
						Env: []corev1.EnvVar{{
							Name:  "DOCKER_CONFIG",
							Value: "/tekton/home/.docker",
						}},
						Args: append([]string{
							"--dockerfile=" + filepath.Join("/workspace", opts.Path, opts.Dockerfile),

							// We expand into /workspace, and publish to the specified
							// output resource image.
							"--context=" + filepath.Join("/workspace", opts.Path),
							"--destination=" + target.Name(),

							// Write out the digest to the appropriate result file.
							"--digest-file=" + digestFile,

							// Enable kanikache to get incremental builds
							"--cache=true",
							"--cache-ttl=24h",
						}, opts.KanikoArgs...),
					},
				}),
			},
		},
	}
}
