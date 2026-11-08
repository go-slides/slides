package themepack

import "io/fs"

// toFileMode turns unix mode bits (as the external attributes of a zip carry
// them) into an fs.FileMode.
func toFileMode(m uint32) fs.FileMode {
	mode := fs.FileMode(m & 0o777)
	switch m & 0o170000 {
	case 0o120000:
		mode |= fs.ModeSymlink
	case 0o60000:
		mode |= fs.ModeDevice
	case 0o20000:
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case 0o10000:
		mode |= fs.ModeNamedPipe
	case 0o40000:
		mode |= fs.ModeDir
	}
	return mode
}
