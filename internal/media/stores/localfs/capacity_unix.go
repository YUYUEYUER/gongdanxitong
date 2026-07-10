//go:build !windows

package fs

import "syscall"

func (c *Client) AvailableBytes() (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(getDir(c.opts.UploadPath), &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
