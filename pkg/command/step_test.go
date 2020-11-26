package command_test

import (
	"context"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jenkins-x/jx-helpers/v3/pkg/files"
	"github.com/mattmoor/mink/pkg/command"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

var (
	gitURL         = "https://github.com/mattmoor/mink"
	expectedDigest = "sha256:8e65ec4b80519d869e8d600fdf262c6e8cd3f6c7e8382406d9cb039f352a69bc"
)

func TestCommandStep(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "")
	require.NoError(t, err, "could not create temp dir")

	t.Logf("running tests in %s\n", tmpDir)

	testData := filepath.Join("test_data", "step")

	testCases := []struct {
		name           string
		image          string
		filenames      []string
		resolvePath    []string
		expectedImages []string
	}{
		{
			name:           "dockerfile",
			image:          "gcr.io/jenkins-x-labs-bdd/myimage:latest",
			filenames:      []string{filepath.Join("charts", "myapp", "values.yaml")},
			resolvePath:    []string{"image", "fullName"},
			expectedImages: []string{"gcr.io/jenkins-x-labs-bdd/myimage:latest@" + expectedDigest},
		},
		{
			name:  "multiple",
			image: "gcr.io/jenkins-x-labs-bdd/$DIR_NAME:latest",
			filenames: []string{
				filepath.Join("helloworld-go", "service.yaml"),
				filepath.Join("helloworld-nodejs", "service.yaml"),
				filepath.Join("helloworld-php", "service.yaml"),
			},
			resolvePath: []string{"spec", "template", "spec", "containers", "[name=main]", "image"},
			expectedImages: []string{"" +
				"gcr.io/jenkins-x-labs-bdd/helloworld-go:latest@" + expectedDigest,
				"gcr.io/jenkins-x-labs-bdd/helloworld-nodejs:latest@" + expectedDigest,
				"gcr.io/jenkins-x-labs-bdd/helloworld-php:latest@" + expectedDigest,
			},
		},
	}

	for _, tc := range testCases {
		name := tc.name
		image := tc.image
		srcDir := filepath.Join(testData, name)
		require.DirExists(t, srcDir)

		destDir := filepath.Join(tmpDir, name)
		err = files.CopyDirOverwrite(srcDir, destDir)
		require.NoError(t, err, "failed to copy %s to %s", srcDir, destDir)

		o := &command.StepOptions{}
		cmd := command.NewStepCommand()
		args := []string{
			"--directory", destDir,
			"--git-url", gitURL,
			"--image", image,
			"--no-git",
			"--local-kaniko",
			"--kaniko-binary", filepath.Join("test_data", "kaniko.sh"),
		}

		for _, f := range tc.filenames {
			fileName := filepath.Join(destDir, f)
			args = append(args, "--filename", fileName)
		}

		err = cmd.ParseFlags(args)
		require.NoError(t, err, "failed to parse flags")
		err = o.Validate(cmd, args)
		require.NoError(t, err, "failed to validate command")

		o.Ctx = context.TODO()
		err = o.Execute(cmd, nil)
		require.NoError(t, err, "failed for test %s", name)

		t.Logf("test %s running in dir %s\n", name, destDir)

		require.Len(t, tc.expectedImages, len(tc.filenames), "expected image should match the number of files")

		for i, f := range tc.filenames {
			fileName := filepath.Join(destDir, f)
			require.FileExists(t, fileName, "the file name should exist")
			assertYamlFileHasStringValue(t, fileName, tc.expectedImages[i], tc.resolvePath...)
		}
	}
}

// assertYamlFileHasStringValue asserts that the yaml file cna be parsed, the paths evaluated to the given expectedText value
func assertYamlFileHasStringValue(t *testing.T, f, expectedText string, paths ...string) {
	node, err := yaml.ReadFile(f)
	require.NoError(t, err, "failed to load file %s", f)

	pathText := strings.Join(paths, ".")

	v, err := node.Pipe(yaml.Lookup(paths...))
	require.NoError(t, err, "failed to evaluate path %s on file %s", pathText, f)
	require.NotNil(t, v, "for path %s on file %s", pathText, f)
	text, err := v.String()
	require.NoError(t, err, "failed to evaluate string of results of path %s on file %s", pathText, f)
	text = strings.TrimSpace(text)
	assert.Equal(t, expectedText, text, "for path %s on file %s", pathText, f)

	t.Logf("evaluated path %s in file %s and found value: %s\n", pathText, f, text)
}
