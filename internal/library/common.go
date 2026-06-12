package library

func newIfPresent[T ~string](new, old T) T {
	if new != "" {
		return new
	} else {
		return old
	}
}
