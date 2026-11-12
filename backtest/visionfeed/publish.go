package visionfeed

import (
	"fmt"
	"os"
	"path/filepath"
)

// PublishVerifiedFile moves a data file and its unchanged checksum sidecar
// into dir under their final names. The move uses exclusive create, so a name
// that is already there keeps its bytes and the call fails. The destination
// directory must not be the directory Load scans: Load ignores subdirectories,
// and the final names must not appear there until both files are in place.
//
// Both files must already pass the same checksum rule Load uses. On failure
// nothing is left under the final names.
func PublishVerifiedFile(dir, dataPath, checksumPath string) error {
	base := filepath.Base(dataPath)
	if _, err := verifyChecksum(dataPath); err != nil {
		return err
	}
	if filepath.Base(checksumPath) != base+".CHECKSUM" {
		return fmt.Errorf("visionfeed: checksum: %s does not belong to %s", filepath.Base(checksumPath), base)
	}
	destData := filepath.Join(dir, base)
	destChecksum := destData + ".CHECKSUM"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("visionfeed: publish: create %s: %w", dir, err)
	}
	if err := linkExclusive(checksumPath, destChecksum); err != nil {
		return fmt.Errorf("visionfeed: publish: %s: %w", base+".CHECKSUM", err)
	}
	if err := linkExclusive(dataPath, destData); err != nil {
		os.Remove(destChecksum)
		return fmt.Errorf("visionfeed: publish: %s: %w", base, err)
	}
	if _, err := verifyChecksum(destData); err != nil {
		os.Remove(destData)
		os.Remove(destChecksum)
		return err
	}
	return nil
}

func linkExclusive(source, dest string) error {
	body, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(body); err != nil {
		file.Close()
		os.Remove(dest)
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(dest)
		return err
	}
	return file.Close()
}
