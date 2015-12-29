package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/drone/drone-go/drone"
	"github.com/drone/drone-go/plugin"
)

const CacheDir = "/cache"

type Cache struct {
	Archive string   `json:"compression"`
	Mount   []string `json:"mount"`
}

func main() {
	workspace := drone.Workspace{}
	repo := drone.Repo{}
	build := drone.Build{}
	job := drone.Job{}
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
			err := restore(hash_, mount, vargs.Archive)
			if err != nil {
				fmt.Printf("Unable to restore %s. %s\n", mount, err)

				// if a cached file is corrupted we should remove it immediately
				// so that subsequent builds don't try to extract.
				purge(hash_, vargs.Archive, 0)
			}

			// restore from repository default branch if possible
			if err != nil && build.Branch != repo.Branch {

				// recalulate the hash using the default branch
				hash_ = hash(mount, repo.Branch, job.Environment)
				fmt.Printf("Restoring cache from %s branch\n", repo.Branch)

				err = restore(hash_, mount, vargs.Archive) // second time is the charm
				if err != nil {
					fmt.Printf("Unable to restore %s from %s branch.\n", mount, repo.Branch)

					// if a cached file is corrupted we should remove it immediately
					// so that subsequent builds don't try to extract.
					purge(hash_, vargs.Archive, 0)
				}
			}
		}
	}

	// if the job is complete and is NOT a pull
	// request we should re-build the cache.
	if isSuccess(&job) && build.Event == drone.EventPush {

		for _, mount := range vargs.Mount {
			// unique hash for the file
			hash_ := hash(mount, build.Branch, job.Environment)
			fmt.Println("Building cache", mount)

			// rebuild
			err := rebuild(hash_, mount, vargs.Archive)
			if err != nil {
				fmt.Printf("Unable to rebuild cache for %s. %s\n", mount, err)
			}
			// purges previously cached files
			purge(hash_, vargs.Archive, 1)
		}
	}
}

func restore(hash, dir, archive string) error {
	tar := fileName(hash, archive)
	_, err := os.Stat(tar)
	if err != nil {
		return fmt.Errorf("Cache does not exist")
	}

	opt := untarOpts(archive)
	cmd := exec.Command("tar", opt, tar, "-C", "/")
	return cmd.Run()
}

// rebuild will rebuild the cache
func rebuild(hash, dir, archive string) (err error) {
	dir = filepath.Clean(dir)
	dir, err = filepath.Abs(dir)
	if err != nil {
		return err
	}
	_, err = os.Stat(dir)
	if err != nil {
		return fmt.Errorf("File or directory %s does not exist", dir)
	}

	opt := tarOpts(archive)
	out := fileName(hash, archive)
	cmd := exec.Command("tar", opt, out, dir)
	return cmd.Run()
}

// purge will purge stale data in the cache to avoid a large
// buildup that could waste disk space on the host machine.
func purge(hash, archive string, keep int) error {
	files, err := list(hash, archive)
	if err != nil {
		return err
	}

	for i := keep; i < len(files); i++ {
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
func list(hash, archive string) ([]string, error) {
	glob := fileName(hash, archive)
	tars, err := filepath.Glob(glob)
	if err != nil {
		return tars, err
	}
	sort.Strings(tars) // sort by date instead?
	return tars, err
}

func tarOpts(archive string) string {
	switch archive {
	case "bzip", "bzip2":
		return "-cjf"
	case "gzip":
		return "-czf"
	default:
		return "-cf"
	}
}

func untarOpts(archive string) string {
	switch archive {
	case "bzip", "bzip2":
		return "-xjf"
	case "gzip":
		return "-xzf"
	default:
		return "-xf"
	}
}

func fileName(hash, archive string) string {
	suffix := fileSuffix(archive)
	return fmt.Sprintf("%s/cache.%s.%s", CacheDir, hash, suffix)
}

func fileSuffix(archive string) string {
	switch archive {
	case "bzip", "bzip2":
		return "tar.bz2"
	case "gzip":
		return "tar.gz"
	default:
		return "tar"
	}
}

func isRunning(job *drone.Job) bool {
	return job.Status == drone.StatusPending ||
		job.Status == drone.StatusRunning
}

func isSuccess(job *drone.Job) bool {
	return job.Status == drone.StatusSuccess
}
