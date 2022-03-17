package version

import "fmt"

type Version struct {
	Major uint8
	Minor uint8
	Patch uint8
}

type unknownError struct {
	version *Version
}

func (v *Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func (e *unknownError) Error() string {
	return fmt.Sprintf("unknown version: %v", e.version)
}

func UnknownError(v *Version) error {
	return &unknownError{v}
}
