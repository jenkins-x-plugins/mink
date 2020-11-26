package command_test

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/jenkins-x/jx-helpers/v3/pkg/files"
	"github.com/jenkins-x/jx-helpers/v3/pkg/testhelpers"
	"github.com/mattmoor/mink/pkg/command"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandInit(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "")
	require.NoError(t, err, "could not create temp dir")

	t.Logf("running tests in %s\n", tmpDir)

	testData := filepath.Join("test_data", "init")
	fs, err := ioutil.ReadDir(testData)

	for _, f := range fs {
		if !f.IsDir() {
			continue
		}
		name := f.Name()
		srcDir := filepath.Join(testData, name)
		require.DirExists(t, srcDir)

		destDir := filepath.Join(tmpDir, name)
		err = files.CopyDirOverwrite(srcDir, destDir)
		require.NoError(t, err, "failed to copy %s to %s", srcDir, destDir)

		cmd := command.NewInitCommand()
		o := &command.InitOptions{
			Directory:  destDir,
			Dockerfile: "Dockerfile",
			NoGit:      true,
		}
		err = o.Execute(cmd, nil)
		require.NoError(t, err, "failed for test %s", name)

		if name == "no-image" {
			assert.NoFileExists(t, filepath.Join(destDir, ".mink.yaml"), "file should not exist for %s", name)
		} else {
			testhelpers.AssertTextFilesEqual(t, filepath.Join(destDir, "expected", ".mink.yaml"), filepath.Join(destDir, ".mink.yaml"), "for test "+name)
			testhelpers.AssertTextFilesEqual(t, filepath.Join(destDir, "expected", "values.yaml"), filepath.Join(destDir, "charts/myapp/values.yaml"), "for test "+name)
		}
	}
}
