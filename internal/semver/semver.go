package semver

// Copied from PremAI/Ayup

import "fmt"

type Version struct {
	major int
	minor int
	patch int
	label []byte
}

func (s Version) IsZero() bool {
	return s.major == 0 && s.minor == 0 && s.patch == 0
}

func Parse(bs []byte) (Version, error) {
	v := Version{}
	dotCount := 0
	num := 0

	for i, b := range bs {
		if b == '.' {
			dotCount += 1

			switch dotCount {
			case 1:
				v.major = num
			case 2:
				v.minor = num
			default:
				return v, fmt.Errorf("Too many dots in semver")
			}

			num = 0
			continue
		}

		if b == '\r' || b == '\n' {
			break
		}

		if b == '-' {
			v.label = bs[i+1:]
			break
		}

		if b >= '0' && b <= '9' {
			num = num*10 + (int(b) - 48)
		} else {
			return v, fmt.Errorf("char '%c' not part of semver", b)
		}
	}

	v.patch = num

	if dotCount < 2 {
		return v, fmt.Errorf("not enough dots in semver")
	}

	if v.IsZero() {
		return v, fmt.Errorf("version zero (0.0.0) is not supported")
	}

	return v, nil
}

// For now this is just used for validation
// var currentVersion Version

func SetVersion(bs []byte) (err error) {
	// if !currentVersion.IsZero() {
	// 	return fmt.Errorf("Version can only be set once")
	// }

	_, err = Parse(bs)

	return err
}

