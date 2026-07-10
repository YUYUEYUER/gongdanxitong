//go:build windows

package fs

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func (c *Client) AvailableBytes() (uint64, error) {
	path, err := filepath.Abs(getDir(c.opts.UploadPath))
	if err != nil {
		return 0, err
	}
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &available, nil, nil); err != nil {
		return 0, err
	}
	return available, nil
}
