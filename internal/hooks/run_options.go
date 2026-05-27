package hooks

// Hook returns the native hook registered for name, or nil.
func (o RunOptions) Hook(name string) Hook {
	switch name {
	case "GlobalSetup":
		return o.GlobalSetup
	case "GlobalTeardown":
		return o.GlobalTeardown
	case "IterationSetup":
		return o.IterationSetup
	case "IterationTeardown":
		return o.IterationTeardown
	default:
		return nil
	}
}
