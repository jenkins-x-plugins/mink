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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dprotaso/go-yit"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/mattmoor/mink/pkg/builds"
	"github.com/mattmoor/mink/pkg/builds/buildpacks"
	"github.com/mattmoor/mink/pkg/builds/dockerfile"
	"github.com/mattmoor/mink/pkg/builds/ko"
	"github.com/mattmoor/mink/pkg/source"
	errs "github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tektoncd/cli/pkg/cli"
	"github.com/tektoncd/cli/pkg/options"
	tknv1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/pool"
	"knative.dev/pkg/signals"
)

var resolveExample = fmt.Sprintf(`
  # Build and publish references within foo.yaml, and print a new version to STDOUT.
  %[1]s resolve -f config/foo.yaml

  # Build and publish references within yaml files recursively under config/, and
  # print a new version to STDOUT.
  %[1]s resolve -Rf config/

  # Customize the builder used for buildpack:/// builds
  %[1]s resolve -f config/ --builder foo:latest

  # Customize the name of the overrides file used for buildpack:/// builds
  %[1]s resolve -f config/ --overrides another-name.toml

  # Customize the name of Dockerfiles to use for dockerfile:/// builds
  %[1]s resolve -f config/ --dockerfile Dockerfile.production`, ExamplePrefix())

// NewResolveCommand implements 'kn-im resolve' command
func NewResolveCommand() *cobra.Command {
	opts := &ResolveOptions{}

	cmd := &cobra.Command{
		Use:     "resolve -f FILE",
		Short:   "Build publish and resolve image references within a collection of yaml files.",
		Example: resolveExample,
		PreRunE: opts.Validate,
		RunE:    opts.Execute,
	}

	opts.AddFlags(cmd)

	return cmd
}

type builder func(context.Context, []tknv1beta1.Step, []name.Reference, *url.URL) (name.Digest, error)

// ResolveOptions implements Interface for the `kn im resolve` command.
type ResolveOptions struct {
	// Inherit all of the base build options.
	BaseBuildOptions

	// Inherit the dockerfile and buildpack options.
	dockerfileOptions
	buildpackOptions

	Filenames []string
	Recursive bool

	// LocalKaniko invoke kaniko locally rather that using a TaskRun. Typically used inside a Pipeline
	LocalKaniko bool

	// KanikoBinary the kaniko binary to use if performing local builds
	KanikoBinary string

	// AllowNoFiles with this setting it is valid to have no files to resolve
	AllowNoFiles bool

	Parallelism int

	// OutputDir specifies the directory to write the resolved yaml files to.
	// If the current directory then the files will be overwritten with the resolved values
	// Use a separate directory to avoid modifying the source
	OutputDir string

	// FlattenOutput if there are multiple YAML files to output flatten them all into files in the output directory
	// This is particularly useful if creating a Helm chart of all the YAML output files where you typically just want
	// to put all the different YAML file names in the charts/mychart/templates folder
	FlattenOutput bool

	builders map[string]builder
	cmd      *cobra.Command
}

// ResolveOptions implements Interface
var _ Interface = (*ResolveOptions)(nil)

// AddFlags implements Interface
func (opts *ResolveOptions) AddFlags(cmd *cobra.Command) {
	// Add the bundle flags to our surface.
	opts.BaseBuildOptions.AddFlags(cmd)
	opts.dockerfileOptions.AddFlags(cmd)
	opts.buildpackOptions.AddFlags(cmd)

	// Based on the same flags in kubectl / ko
	cmd.Flags().StringSliceP("filename", "f", nil,
		"Filename, directory, or URL to files to use to create the resource")
	cmd.Flags().BoolP("recursive", "R", false,
		"Process the directory used in -f, --filename recursively. Useful when you want to manage related manifests organized within the same directory.")
	cmd.Flags().IntP("parallelism", "P", 20, "How many parallel builds to run at once.")
	cmd.Flags().StringP("output", "O", "", "Output directory for resolved YAML files to be written. If the current directory then the YAML files are resolved and modified in place. Otherwise the files are written to the output directory using relative paths from the current directory.")
	cmd.Flags().Bool("flatten-output", false, "Put all of the output files into the output directory as files without a tree of directories. This is particularly useful if creating a Helm chart of all the YAML output files where you typically just want to put all the different YAML file names in, say, the charts/mychart/templates folder")
	cmd.Flags().BoolP("local-kaniko", "L", false,
		"Uses a local kaniko binary for building Dockerfile based builds instead of a separate TaskRun.")
	cmd.Flags().StringP("kaniko-binary", "", "/kaniko/executor", "The kaniko/executor binary location if using local builds.")
}

// Validate implements Interface
func (opts *ResolveOptions) Validate(cmd *cobra.Command, args []string) error {
	// disable validation of the image string as it may have expressions which we resolve via each filename...
	opts.BaseBuildOptions.NoImageValidate = true

	// Validate the bundle arguments.
	if err := opts.BaseBuildOptions.Validate(cmd, args); err != nil {
		return err
	}
	if err := opts.dockerfileOptions.Validate(cmd, args); err != nil {
		return err
	}
	if err := opts.buildpackOptions.Validate(cmd, args); err != nil {
		return err
	}

	opts.Filenames = viper.GetStringSlice("filename")
	if len(opts.Filenames) == 0 {
		if !opts.AllowNoFiles {
			return apis.ErrMissingField("filename")
		}
	}
	opts.Recursive = viper.GetBool("recursive")

	opts.Parallelism = viper.GetInt("parallelism")
	if opts.Parallelism <= 0 {
		return apis.ErrInvalidValue(opts.Parallelism, "parallelism")
	}

	opts.LocalKaniko = viper.GetBool("local-kaniko")

	opts.OutputDir = viper.GetString("output")
	opts.FlattenOutput = viper.GetBool("flatten-output")

	opts.KanikoBinary = viper.GetString("kaniko-binary")

	opts.builders = map[string]builder{
		"dockerfile": opts.db,
		"buildpack":  opts.bp,
		"ko":         opts.ko,
	}

	opts.cmd = cmd
	return nil
}

// Execute implements Interface
func (opts *ResolveOptions) Execute(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return errors.New("'im bundle' does not take any arguments")
	}

	// Handle ctrl+C
	return opts.execute(signals.NewContext(), cmd)
}

// execute is the workhorse of execute, but factored to support composition
// with apply (provides its own ctx)
func (opts *ResolveOptions) execute(ctx context.Context, cmd *cobra.Command) error {
	// Bundle up the source context in an image or use git clone to get the source.
	sourceSteps, nameRefs, err := source.CreateSourceSteps(ctx, opts.Directory, opts.BundleOptions.tag, opts.BundleOptions.GitLocation)
	if err != nil {
		return err
	}

	// Turn the files into yaml nodes.
	files := opts.EnumerateFiles()
	fileBlocks := make([]*yamlDoc, 0, len(files))
	for _, f := range files {
		bs, err := opts.ResolveFile(ctx, f)
		if err != nil {
			return err
		}
		fileBlocks = append(fileBlocks, &yamlDoc{path: f, blocks: bs})
	}

	// Turn all of the images references in the yaml nodes into digests.
	if err := opts.ResolveReferences(ctx, fileBlocks, sourceSteps, nameRefs); err != nil {
		return err
	}

	// Encode the resulting yaml
	for _, fb := range fileBlocks {
		out, err := opts.writeForFile(fb.path)
		if err != nil {
			return err
		}
		var e *yaml.Encoder
		if out != nil {
			defer out.Close()
			e = yaml.NewEncoder(out)
		} else {
			e = yaml.NewEncoder(cmd.OutOrStdout())
		}
		e.SetIndent(2)
		for _, doc := range fb.blocks {
			if err := e.Encode(doc); err != nil {
				return fmt.Errorf("failed to encode output: %w", err)
			}
		}
		e.Close()
	}
	return nil
}

func (opts *ResolveOptions) writeForFile(path string) (io.WriteCloser, error) {
	if opts.OutputDir != "" {
		dir := opts.Directory
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return nil, errs.Wrapf(err, "failed to find absolute path for %s", dir)
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, errs.Wrapf(err, "failed to find absolute path for %s", path)
		}
		rel, err := filepath.Rel(absDir, absPath)
		if err != nil {
			return nil, errs.Wrapf(err, "failed to find relative path for %s from %s", absPath, absDir)
		}
		if opts.FlattenOutput {
			rel = strings.ReplaceAll(rel, string(os.PathSeparator), "-")
		}
		f := filepath.Join(opts.OutputDir, rel)
		d := filepath.Dir(f)
		err = os.MkdirAll(d, 0760)
		if err != nil {
			return nil, errs.Wrapf(err, "failed to create output directory %s", d)
		}
		log.Printf("writing file %s\n", f)
		return os.Create(f)
	}
	return nil, nil
}

type yamlDoc struct {
	path   string
	blocks []*yaml.Node
}

// EnumerateFiles is based heavily on pkg/kubectl
func (opts *ResolveOptions) EnumerateFiles() (files []string) {
	seen := sets.NewString()
	for _, paths := range opts.Filenames {
		// Just pass through '-' as it is indicative of stdin.
		if paths == "-" {
			files = append(files, paths)
			continue
		}
		// For each of the "filenames" we are passed (file or directory) start a
		// "Walk" to enumerate all of the contained files recursively.
		err := filepath.Walk(paths, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if seen.Has(path) {
				return nil
			}

			// If this is a directory, skip it if it isn't the current directory we are
			// processing (unless we are in recursive mode).
			if fi.IsDir() {
				if path != paths && !opts.Recursive {
					return filepath.SkipDir
				}
				return nil
			}

			// Don't check extension if the filepath was passed explicitly
			if path != paths {
				switch filepath.Ext(path) {
				case ".json", ".yaml":
					// Process these.
				default:
					return nil
				}
			}

			files = append(files, path)
			seen.Insert(path)
			return nil
		})
		if err != nil {
			log.Fatal("Error enumerating files:", err)
		}
	}
	return files
}

// ResolveFile is based heavily on ko's resolveFile
func (opts *ResolveOptions) ResolveFile(ctx context.Context, f string) (blocks []*yaml.Node, err error) {
	var b []byte
	if f == "-" {
		b, err = ioutil.ReadAll(os.Stdin)
	} else {
		b, err = ioutil.ReadFile(f)
	}
	if err != nil {
		return nil, err
	}

	// The loop is to support multi-document yaml files.
	// This is handled by using a yaml.Decoder and reading objects until io.EOF, see:
	// https://godoc.org/gopkg.in/yaml.v3#Decoder.Decode
	decoder := yaml.NewDecoder(bytes.NewBuffer(b))
	for {
		var doc yaml.Node
		if err := decoder.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}

		blocks = append(blocks, &doc)
	}

	return
}

// ResolveReferences is based heavily on ko's ImageReferences
func (opts *ResolveOptions) ResolveReferences(ctx context.Context, fileBlocks []*yamlDoc, sourceSteps []tknv1beta1.Step, nameRefs []name.Reference) error {
	// First, walk the input objects and collect a list of supported references
	refs := make(map[string][]*yaml.Node)

	for _, fb := range fileBlocks {
		for _, doc := range fb.blocks {
			it := opts.refsFromDoc(doc)

			for node, ok := it(); ok; node, ok = it() {
				ref := strings.TrimSpace(node.Value)
				refs[ref] = append(refs[ref], node)
			}
		}
	}

	errg, ctx := pool.NewWithContext(ctx, opts.Parallelism, opts.Parallelism)

	// Next, perform parallel builds for each of the supported references.
	var sm sync.Map
	for ref := range refs {
		ref := ref

		// Parse the reference and use the scheme to determine
		// the builder to apply.
		u, err := url.Parse(ref)
		if err != nil {
			return err
		}
		builder, ok := opts.builders[u.Scheme]
		if !ok {
			continue
		}

		errg.Go(func() error {
			digest, err := builder(ctx, sourceSteps, nameRefs, u)
			if err != nil {
				return err
			}
			sm.Store(ref, digest.String())
			return nil
		})
	}
	if err := errg.Wait(); err != nil {
		return err
	}

	// Walk the tags and update them with their digest.
	for ref, nodes := range refs {
		digest, ok := sm.Load(ref)

		if !ok {
			return fmt.Errorf("resolved reference to %q not found", ref)
		}

		for _, node := range nodes {
			node.Value = digest.(string)
		}
	}
	return nil
}

func (opts *ResolveOptions) db(ctx context.Context, sourceSteps []tknv1beta1.Step, nameRefs []name.Reference, u *url.URL) (name.Digest, error) {
	if u.Host != "" {
		return name.Digest{}, fmt.Errorf(
			"unexpected host in %q reference, got: %s (did you mean %s:/// instead of %s://?)",
			u.Scheme, u.Host, u.Scheme, u.Scheme)
	}

	// TODO(mattmoor): Consider merging in some "path"-specific configuration here.
	// My fundamental conflict is that I'd like for `mink buildpack` to be consistent,
	// and they have different views of the filesystem (more will work here)...

	path := u.Path

	imageName, tag, err := opts.ResolveImageName(path)
	if err != nil {
		return name.Digest{}, err
	}
	fmt.Fprintf(opts.cmd.OutOrStdout(), "building image %s\n", imageName)

	digestFile := dockerfile.DigestFile
	if opts.LocalKaniko {
		// lets make sure we have a tmp dir as it may not exist yet if inside, say, a kaniko image
		tmpDir := os.TempDir()
		err = os.MkdirAll(tmpDir, 0760)
		if err != nil {
			return name.Digest{}, errs.Wrapf(err, "failed to create temp dir %s", tmpDir)
		}
		tmpFile, err := ioutil.TempFile("", "mink-digest-")
		if err != nil {
			return name.Digest{}, errs.Wrapf(err, "failed to create temp digest file")
		}
		digestFile = tmpFile.Name()
	}
	tr := dockerfile.Build(ctx, sourceSteps, tag, dockerfile.Options{
		Dockerfile: opts.Dockerfile,
		Path:       path,
		DigestFile: digestFile,
		KanikoArgs: opts.KanikoArgs,
	})
	tr.Namespace = Namespace()

	// Buffer the output, so we can display it on failures.
	var buf *bytes.Buffer
	var out io.Writer
	if opts.OutputDir == "" {
		buf = &bytes.Buffer{}
		// lets only log output on errors so we can pipe the output to kubectl etc
		out = buf
	} else {
		out = opts.cmd.OutOrStderr()
	}

	if opts.LocalKaniko {
		return opts.runLocalBuild(tr, opts.KanikoBinary, imageName, path, digestFile)
	}
	// Run the produced Build definition to completion, streaming logs to stdout, and
	// returning the digest of the produced image.
	digest, err := builds.Run(ctx, imageName, tr, &options.LogOptions{
		Params: &cli.TektonParams{},
		Stream: &cli.Stream{
			Out: out,
			Err: out,
		},
		Follow: true,
	}, builds.WithServiceAccount(opts.ServiceAccount, nameRefs...))
	if err != nil {
		if buf != nil {
			log.Print(buf.String())
		}
		return name.Digest{}, err
	}
	return digest, nil
}

// ResolveImageName allows environment variables to be used in the image string along with expressions for the
// current directory name
func (opts *ResolveOptions) ResolveImageName(path string) (string, name.Tag, error) {
	_, dirName := filepath.Split(path)
	image := os.Expand(opts.ImageName, func(name string) string {
		answer := os.Getenv(name)
		if answer == "" && name == "DIR_NAME" {
			answer = dirName
		}
		return answer
	})
	tag, err := name.NewTag(image, name.WeakValidation)
	if err != nil {
		return image, tag, apis.ErrInvalidValue(err.Error(), "image")
	}
	return image, tag, nil
}

func (opts *ResolveOptions) bp(ctx context.Context, sourceSteps []tknv1beta1.Step, nameRefs []name.Reference, u *url.URL) (name.Digest, error) {
	if u.Host != "" {
		return name.Digest{}, fmt.Errorf(
			"unexpected host in %q reference, got: %s (did you mean %s:/// instead of %s://?)",
			u.Scheme, u.Host, u.Scheme, u.Scheme)
	}

	// TODO(mattmoor): Consider merging in some "path"-specific configuration here.
	// My fundamental conflict is that I'd like for `mink buildpack` to be consistent,
	// and they have different views of the filesystem (more will work here)...

	// Parse our KEY=VALUE from the Host/Path combo.
	// TODO(mattmoor): What if this was a path to a project.toml with overrides instead?
	// parts := strings.SplitN(u.Host+u.Path, "=", 2)
	// if len(parts) != 2 {
	// 	parts = append(parts, "")
	// }
	// key, value := parts[0], parts[1]

	tr := buildpacks.Build(ctx, sourceSteps, opts.tag, buildpacks.Options{
		Builder:      opts.Builder,
		OverrideFile: opts.OverrideFile,
		Path:         u.Path,
		// Env: []corev1.EnvVar{{
		// 	Name:  key,
		// 	Value: value,
		// }},
	})
	tr.Namespace = Namespace()

	// Buffer the output, so we can display it on failures.
	buf := &bytes.Buffer{}

	// Run the produced Build definition to completion, streaming logs to stdout, and
	// returning the digest of the produced image.
	digest, err := builds.Run(ctx, opts.ImageName, tr, &options.LogOptions{
		Params: &cli.TektonParams{},
		Stream: &cli.Stream{
			// Send Out to stderr so we can capture the digest for composition.
			Out: buf,
			Err: buf,
		},
		Follow: true,
	}, builds.WithServiceAccount(opts.ServiceAccount, nameRefs...))
	if err != nil {
		log.Print(buf.String())
		return name.Digest{}, err
	}
	return digest, nil
}

func (opts *ResolveOptions) ko(ctx context.Context, sourceSteps []tknv1beta1.Step, nameRefs []name.Reference, u *url.URL) (name.Digest, error) {
	// TODO(mattmoor): Consider merging in some "path"-specific configuration here.
	// My fundamental conflict is that I'd like for `mink buildpack` to be consistent,
	// and they have different views of the filesystem (more will work here)...

	tr := ko.Build(ctx, sourceSteps, opts.tag, ko.Options{
		ImportPath: u.String(),
	})
	tr.Namespace = Namespace()

	// Buffer the output, so we can display it on failures.
	buf := &bytes.Buffer{}

	// Run the produced Build definition to completion, streaming logs to stdout, and
	// returning the digest of the produced image.
	digest, err := builds.Run(ctx, opts.ImageName, tr, &options.LogOptions{
		Params: &cli.TektonParams{},
		Stream: &cli.Stream{
			// Send Out to stderr so we can capture the digest for composition.
			Out: buf,
			Err: buf,
		},
		Follow: true,
	}, builds.WithServiceAccount(opts.ServiceAccount, nameRefs...))
	if err != nil {
		log.Print(buf.String())
		return name.Digest{}, err
	}
	return digest, nil
}

func (opts *ResolveOptions) refsFromDoc(doc *yaml.Node) yit.Iterator {
	ps := make([]yit.Predicate, 0, len(opts.builders))

	for k := range opts.builders {
		ps = append(ps, yit.WithPrefix(k+"://"))
	}

	return yit.FromNode(doc).
		RecurseNodes().
		Filter(yit.StringValue).
		Filter(yit.Union(ps...))
}

func (opts *ResolveOptions) runLocalBuild(tr *tknv1beta1.TaskRun, binary, imageName, path, digestFile string) (name.Digest, error) {
	args := tr.Spec.TaskSpec.Steps[len(tr.Spec.TaskSpec.Steps)-1].Args

	// lets replace the context with the current dir
	wd, err := os.Getwd()
	if err != nil {
		return name.Digest{}, errs.Wrapf(err, "failed to get current working directory")
	}

	context := wd
	if path != "" && path != "/" {
		context = filepath.Join(wd, path)
	}
	for i := range args {
		arg := args[i]
		if strings.HasPrefix(arg, "--context=") {
			args[i] = "--context=" + context
		} else if strings.HasPrefix(arg, "--dockerfile=/workspace") {
			args[i] = "--dockerfile=" + wd + arg[len("--dockerfile=/workspace"):]
		}
	}
	argsText := strings.Join(args, " ")

	dir := filepath.Dir(digestFile)
	err = os.MkdirAll(dir, 0760)
	if err != nil {
		return name.Digest{}, errs.Wrapf(err, "failed to make dir %s", dir)
	}

	log.Printf("running: %s %s\n", binary, argsText)

	c := exec.Command(binary, args...)

	// Pass through our environment
	c.Env = os.Environ()
	c.Stderr = os.Stderr
	c.Stdout = os.Stdout
	c.Stdin = os.Stdin
	err = c.Run()
	if err != nil {
		return name.Digest{}, errs.Wrapf(err, "failed to run command: %s %s", binary, argsText)
	}

	data, err := ioutil.ReadFile(digestFile)
	if err != nil {
		return name.Digest{}, errs.Wrapf(err, "failed to read %s", digestFile)
	}

	value := strings.TrimSpace(string(data))
	return name.NewDigest(imageName + "@" + value)
}
