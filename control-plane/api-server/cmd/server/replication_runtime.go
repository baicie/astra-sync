package main

// configuredReplicationTopology supplies the static standby decision used by
// the promotion manager until the topology watcher backend is enabled.
type configuredReplicationTopology struct {
	current string
	standby string
}

func (t configuredReplicationTopology) IsStandby(region string) bool {
	return region != "" && region == t.standby && region != t.current
}
