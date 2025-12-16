package push

type StatsHandler interface {
	HandleStats(data any) error
}

func registerHandlers(pManager *PushManager) {
	pManager.register("traffic", &Traffic{redis: pManager.redis})
	pManager.register("relation", &Relation{redis: pManager.redis})

}
