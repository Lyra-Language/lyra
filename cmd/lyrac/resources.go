package main

// A program's resources: the folder `lyra.toml`'s `resources` names, holding files the program
// reads when it runs (a game's art). lyrac puts it where the program is — beside the executable,
// in `lyrac run`'s temporary folder, inside an iOS app before it is signed — so the program finds
// it the same way however it was built: under `SDL_GetBasePath`, or its own executable's folder.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// withResources is lowerAndEmit's result for a finished executable: its resources put beside it.
func withResources(exe string, res *driver.Result) (string, int) {
	if err := carryResources(res.Target.Resources, filepath.Dir(exe)); err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return "", 1
	}
	return exe, 0
}

// carryResources makes `dir/<from's name>` a copy of the folder from, replacing what was there.
// No folder named, or none there yet, carries nothing; nor does a build into the project itself,
// where the folder already is.
func carryResources(from, dir string) error {
	if from == "" {
		return nil
	}
	source, err := os.Stat(from)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !source.IsDir() {
		return fmt.Errorf("resources: %s is not a folder", from)
	}
	to := filepath.Join(dir, filepath.Base(from))
	if there, err := os.Stat(to); err == nil && os.SameFile(source, there) {
		return nil
	}
	if err := os.RemoveAll(to); err != nil {
		return err
	}
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
