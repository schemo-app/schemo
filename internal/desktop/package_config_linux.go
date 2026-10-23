package desktop

func configurePackage(engine *Updater, host *Host) bool {
	target, err := installTarget()
	if err != nil || !packageInstalled(target) {
		return false
	}
	engine.installPackage = installDebPackage
	engine.restart = host.Restart
	return true
}
