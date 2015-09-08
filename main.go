package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/drone/drone-plugin-go/plugin"
)

const CacheDir = "/cache"

type Cache struct {
	Mount []string `json:"mount"`
}

func main() {
	workspace := plugin.Workspace{}
	repo := plugin.Repo{}
	build := plugin.Build{}
	job := plugin.Job{}
	vargs := Cache{}

	plugin.Param("workspace", &workspace)
	plugin.Param("repo", &repo)
	plugin.Param("build", &build)
	plugin.Param("job", &job)
	plugin.Param("vargs", &vargs)
	plugin.MustParse()

	// mount paths are relative to the workspace.
	// if the workspace doesn't exist, create it
	os.MkdirAll(workspace.Path, 0755)
	os.Chdir(workspace.Path)

	// if the job is running we should restore
	// the cache
	if isRunning(&job) {

		for _, mount := range vargs.Mount {
			// only restore the cache if the
			// file actually exists
			_, err := os.Stat(mount)
			if err != nil {
				continue
			}

			// unique hash for the file
			hash_ := hash(mount, build.Commit.Branch, job.Environment)
			fmt.Println("Restoring cache", mount)

			// restore
			err = restore(hash_, mount)
			if err != nil {
				fmt.Printf("Unable to restore %s. %s\n", mount, err)
			}
		}

	}

	// if the job is complete and is NOT a pull
	// request we should re-build the cache.
	if isSuccess(&job) && !plugin.IsPullRequest(&build) {

		for _, mount := range vargs.Mount {
			// unique hash for the file
			hash_ := hash(mount, build.Commit.Branch, job.Environment)

			// rebuild
			err := rebuild(hash_, mount)
			if err != nil {
				fmt.Printf("Unable to rebuild cache for %s. %s\n", mount, err)
			}
			// purges previously cached files
			purge(hash_)
		}
	}
}

func restore(hash, mount string) error {
	tar := fmt.Sprintf("%s/cache.%s.tar.gz", CacheDir, hash)
	cmd := exec.Command("tar", "xf", tar, "-C", mount)
	return cmd.Run()
}

// rebuild will rebuild the cache
func rebuild(hash, mount string) error {
	out := fmt.Sprintf("%s/cache.%s.tar.gz", CacheDir, hash)
	cmd := exec.Command("tar", "cfz", out, mount)
	return cmd.Run()
}

// purge will purge stale data in the cache
// to avoid a large buildup that could waste
// disk space on the host machine.
func purge(hash string) error {
	files, err := list(hash)
	if err != nil {
		return err
	}

	// we should only keep the latest
	// file in the cache
	for i := 1; i < len(files); i++ {
		err = os.Remove(files[i])
		if err != nil {
			return err
		}
	}
	return nil
}

func hash(mount, branch string, matrix map[string]string) string {
	parts := []string{mount, branch}

	// concatinate all matrix values
	// with the branch
	for val := range matrix {
		parts = append(parts, val)
	}

	// sort the strings to ensure ordering
	// is maintained prior to hashing
	sort.Strings(parts)

	// calculate the hash using the branch
	// and matrix combined.
	h := md5.New()
	for _, part := range parts {
		io.WriteString(h, part)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// list returns a list of items in the cache.
func list(hash string) ([]string, error) {
	glob := fmt.Sprintf("%s/cache.%s.tar.gz", CacheDir, hash)
	tars, err := filepath.Glob(glob)
	if err != nil {
		return tars, err
	}
	sort.Strings(tars) // sort by date instead?
	return tars, err
}

func isRunning(job *plugin.Job) bool {
	return job.Status == plugin.StatePending ||
		job.Status == plugin.StateRunning
}

func isSuccess(job *plugin.Job) bool {
	return job.Status == plugin.StateSuccess
}
