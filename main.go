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
			// unique hash for the file
			hash_ := hash(mount, build.Branch, job.Environment)
			fmt.Println("Restoring cache", mount)

			// restore
			err := restore(hash_, mount)
			if err != nil {
				fmt.Printf("Unable to restore %s. %s\n", mount, err)
			}

			// restore from repository default branch if possible
			if err != nil && build.Branch != repo.Branch {

				// recalulate the hash using the default branch
				hash_ = hash(mount, repo.Branch, job.Environment)
				fmt.Printf("Restoring cache from %s branch\n", repo.Branch)

				err = restore(hash_, mount) // second time is the charm
				if err != nil {
					fmt.Printf("Unable to restore %s from %s branch.\n", mount, repo.Branch)
				}
			}
		}
	}

	// if the job is complete and is NOT a pull
	// request we should re-build the cache.
	if isSuccess(&job) && build.Event == plugin.EventPush {

		for _, mount := range vargs.Mount {
			// unique hash for the file
			hash_ := hash(mount, build.Branch, job.Environment)
			fmt.Println("Building cache", mount)

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

func restore(hash, dir string) error {
	tar := fmt.Sprintf("%s/cache.%s.tar.gz", CacheDir, hash)
	_, err := os.Stat(tar)
	if err != nil {
		return fmt.Errorf("Cache does not exist")
	}

	cmd := exec.Command("tar", "-xzf", tar, "-C", "/")
	return cmd.Run()
}

// rebuild will rebuild the cache
func rebuild(hash, dir string) (err error) {
	dir = filepath.Clean(dir)
	dir, err = filepath.Abs(dir)
	if err != nil {
		return err
	}
	_, err = os.Stat(dir)
	if err != nil {
		return fmt.Errorf("File or directory %s does not exist", dir)
	}

	out := fmt.Sprintf("%s/cache.%s.tar.gz", CacheDir, hash)
	cmd := exec.Command("tar", "-czf", out, dir)
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
	for key, val := range matrix {
		parts = append(parts, key+"="+val)
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
