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
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jenkins-x/jx-helpers/v3/pkg/files"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"knative.dev/pkg/signals"
)

var pkgShort = "Detects images if no .mink.yaml or explicit arguments are supplied, builds any images and resolves any YAML files/"
var pkgLong = pkgShort + `

This command is intended to be used inside a pipeline and can handle repositories which contain 0..N images. It defaults to outputting the modified YAML files with the image digest in place for a new release in a new branch/tag.

If you are running this locally you may want to override the --output value to a different directory. 
`
var pkgExample = fmt.Sprintf(`
  # Generate a .mink.yaml file if it does not exist and a dockerfile or build pack can be detected
  # then build and publish references within .mink.yaml either outputs the YAML or saves it in place
  %[1]s package --image gcr.io/myproject/myimage:latest

  # Resolves the images specified in the filename entries in .mink.yaml and then outputs the resolved 
  # YAML files to charts/mychart/templates/*.yaml for releasing as a helm chart
  %[1]s package --image gcr.io/myproject/\$DIR_NAME:latest --output charts/mychart/templates --flatten-output
`, ExamplePrefix())

// NewPackageCommand implements 'kn-im resolve' command
func NewPackageCommand() *cobra.Command {
	opts := &PackageOptions{}

	cmd := &cobra.Command{
		Use:     "package",
		Aliases: []string{"pkg"},
		Short:   pkgShort,
		Long:    pkgLong,
		Example: pkgExample,
		PreRunE: opts.Validate,
		RunE:    opts.Execute,
	}

	opts.AddFlags(cmd)

	return cmd
}

// PackageOptions implements Interface for the `kn im package` command.
type PackageOptions struct {
	// Inherit all of the resolve options.
	ResolveOptions

	InitOptions InitOptions

	// Out the output destination
	Out io.Writer

	// Ctx allows tests to use a non-Ctrl-c handler for loops in tests
	Ctx context.Context
}

// PackageOptions implements Interface
var _ Interface = (*PackageOptions)(nil)

// AddFlags implements Interface
func (opts *PackageOptions) AddFlags(cmd *cobra.Command) {
	opts.InitOptions.InPackageCommand = true

	// Add the bundle flags to our surface.
	opts.ResolveOptions.AddFlags(cmd)

	opts.InitOptions.AddFlags(cmd)
}

// Validate implements Interface
func (opts *PackageOptions) Validate(cmd *cobra.Command, args []string) error {
	viper.SetDefault("output", ".")
	setViperGitAndKanikoDefaults(cmd.OutOrStdout())

	opts.ResolveOptions.AllowNoFiles = true

	// Validate the bundle arguments.
	if err := opts.ResolveOptions.Validate(cmd, args); err != nil {
		return err
	}

	// InitOptions flags
	if err := opts.InitOptions.Validate(cmd, args); err != nil {
		return err
	}
	return nil
}

// Execute implements Interface
func (opts *PackageOptions) Execute(cmd *cobra.Command, args []string) error {
	// Handle ctrl+C
	if opts.Ctx == nil {
		opts.Ctx = signals.NewContext()
	}
	return opts.execute(opts.Ctx, cmd)
}

// execute is the workhorse of execute, but factored to support composition
// with apply (provides its own ctx)
func (opts *PackageOptions) execute(ctx context.Context, cmd *cobra.Command) error {
	if opts.Out == nil {
		opts.Out = cmd.OutOrStdout()
	}
	err := opts.InitOptions.Execute(cmd, nil)
	if err != nil {
		return errors.Wrapf(err, "failed to detect .mink.yaml file")
	}

	if opts.ResolveOptions.LocalKaniko {
		err = opts.copyKanikoDockerSecrets()
		if err != nil {
			return errors.Wrapf(err, "failed to copy kaniko docker secrets")
		}
	}

	if !opts.InitOptions.MinkEnabled && len(opts.Filenames) == 0 {
		return nil
	}
	return opts.ResolveOptions.execute(ctx, cmd)
}

func (opts *PackageOptions) copyKanikoDockerSecrets() error {
	glob := filepath.Join("/tekton", "creds-secrets", "*", ".dockerconfigjson")
	fs, err := filepath.Glob(glob)
	if err != nil {
		return errors.Wrapf(err, "failed to find tekton secrets")
	}
	if len(fs) == 0 {
		fmt.Fprintf(opts.Out, "failed to find docker secrets %s\n", glob)
		return nil
	}
	srcFile := fs[0]

	outDir := filepath.Join("/kaniko", ".docker")
	err = os.MkdirAll(outDir, files.DefaultDirWritePermissions)
	if err != nil {
		return errors.Wrapf(err, "failed to create dir %s", outDir)
	}
	outFile := filepath.Join(outDir, "config.json")
	err = files.CopyFile(srcFile, outFile)
	if err != nil {
		return errors.Wrapf(err, "failed to copy file %s to %s", srcFile, outFile)
	}

	fmt.Fprintf(opts.Out, "copied secret %s to %s\n", srcFile, outFile)
	return nil
}
