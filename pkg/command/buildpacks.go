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

package command

import (
	"errors"
	"fmt"

	"github.com/mattmoor/mink/pkg/builds"
	"github.com/mattmoor/mink/pkg/builds/buildpacks"
	"github.com/mattmoor/mink/pkg/source"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tektoncd/cli/pkg/cli"
	"github.com/tektoncd/cli/pkg/options"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/signals"
)

var buildpacksExample = fmt.Sprintf(`
  # Create a build from the context in the current directory and publish
  # it as the provided image name.
  %[1]s buildpack --image docker.io/mattmoor/bundle:latest

  # As the first, but executes the build as the service account "builder".
  %[1]s buildpack --as=builder --image docker.io/mattmoor/bundle:latest

  # As the first, but uses the context from the provided directory.
  %[1]s buildpack --directory=subdir/ --image docker.io/mattmoor/bundle:latest

  # As the first, but uses a different builder image.
  %[1]s buildpack --builder=cloudfoundry/cnb:bionic --image docker.io/mattmoor/bundle:latest

  # As the first, but executes the build as a temporary ServiceAccount
  # that is configured with the user's local credentials.
  # WARNING: This temporarily places your registry credentials in a Secret
  # on your cluster, so use this option with caution in shared environments.
  %[1]s buildpack --as=me --image docker.io/mattmoor/bundle:latest`, ExamplePrefix())

// NewBuildpackCommand implements 'kn-im build' command
func NewBuildpackCommand() *cobra.Command {
	opts := &BuildpackOptions{}

	cmd := &cobra.Command{
		Use:     "buildpack --image IMAGE",
		Short:   "Build an image via Cloud Native Buildpacks.",
		Example: buildpacksExample,
		PreRunE: opts.Validate,
		RunE:    opts.Execute,
	}

	opts.AddFlags(cmd)

	return cmd
}

type buildpackOptions struct {
	// Builder is the name of the buildpack builder container image.
	Builder string

	// OverrideFile holds the name of the file that overrides project.toml settings.
	OverrideFile string
}

// AddFlags implements Interface
func (opts *buildpackOptions) AddFlags(cmd *cobra.Command) {
	cmd.Flags().String("builder", buildpacks.BuildpackImage,
		"The name of the builder container image to execute.")

	cmd.Flags().String("overrides", "overrides.toml",
		"The name of the file to read project.toml overrides from.")
}

// Validate implements Interface
func (opts *buildpackOptions) Validate(cmd *cobra.Command, args []string) error {
	opts.Builder = viper.GetString("builder")
	if opts.Builder == "" {
		return apis.ErrMissingField("builder")
	}

	opts.OverrideFile = viper.GetString("overrides")
	if opts.OverrideFile == "" {
		return apis.ErrMissingField("overrides")
	}
	return nil
}

// BuildpackOptions implements Interface for the `kn im build` command.
type BuildpackOptions struct {
	// Inherit all of the base build options.
	BaseBuildOptions

	buildpackOptions
}

// BuildpackOptions implements Interface
var _ Interface = (*BuildpackOptions)(nil)

// AddFlags implements Interface
func (opts *BuildpackOptions) AddFlags(cmd *cobra.Command) {
	// Add the bundle flags to our surface.
	opts.BaseBuildOptions.AddFlags(cmd)

	opts.buildpackOptions.AddFlags(cmd)
}

// Validate implements Interface
func (opts *BuildpackOptions) Validate(cmd *cobra.Command, args []string) error {
	// Validate the bundle arguments.
	if err := opts.BaseBuildOptions.Validate(cmd, args); err != nil {
		return err
	}

	return opts.buildpackOptions.Validate(cmd, args)
}

// Execute implements Interface
func (opts *BuildpackOptions) Execute(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return errors.New("'im bundle' does not take any arguments")
	}

	// Handle ctrl+C
	ctx := signals.NewContext()

	// Bundle up the source context in an image or use git clone to get the source.
	sourceSteps, nameRefs, err := source.CreateSourceSteps(ctx, opts.Directory, opts.BundleOptions.tag, opts.BundleOptions.GitLocation)
	if err != nil {
		return err
	}

	// Create a Build definition for turning the source into an image via CNCF Buildpacks.
	tr := buildpacks.Build(ctx, sourceSteps, opts.tag, buildpacks.Options{
		Builder:      opts.Builder,
		OverrideFile: opts.OverrideFile,
	})
	tr.Namespace = Namespace()

	// Run the produced Build definition to completion, streaming logs to stdout, and
	// returning the digest of the produced image.
	digest, err := builds.Run(ctx, opts.ImageName, tr, &options.LogOptions{
		Params: &cli.TektonParams{},
		Stream: &cli.Stream{
			// Send Out to stderr so we can capture the digest for composition.
			Out: cmd.OutOrStderr(),
			Err: cmd.OutOrStderr(),
		},
		Follow: true,
	}, builds.WithServiceAccount(opts.ServiceAccount, nameRefs...))
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", digest.String())
	return nil
}
