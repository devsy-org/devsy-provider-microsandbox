package msb

func (s *clientSuite) TestStoppedInspectionRetainsSavedConfiguration() {
	info, err := parseInfo(
		[]byte(
			`{"name":"stopped","status":"Stopped","active_config":null,"config":{
 "labels":{"developer":"vscode"},"mounts":[
 {"type":"Bind","host":"/host","guest":"/workspace"},
 {"type":"Tmpfs","guest":"/tmp"},
 {"type":"Named","name":"cache","guest":"/cache"},
 {"type":"Unknown","guest":"/ignored"}]}}`,
		),
	)
	s.Require().NoError(err)
	s.False(info.Running)
	s.Equal("vscode", info.Labels["developer"])
	s.Require().Len(info.Mounts, 3)
	s.Equal("/host", info.Mounts[0].Source)
	s.Equal("/workspace", info.Mounts[0].Target)
	s.True(info.Mounts[1].Tmpfs)
	s.Equal("cache", info.Mounts[2].Volume)
	s.Equal("/cache", info.Mounts[2].Target)
}

func (s *clientSuite) TestActiveInspectionTakesPrecedence() {
	info, err := parseInfo(
		[]byte(
			`{"name":"running","status":"Running",
 "active_config":{"labels":{"developer":"active"}},"config":{"labels":{"developer":"saved"}}}`,
		),
	)
	s.Require().NoError(err)
	s.True(info.Running)
	s.Equal("active", info.Labels["developer"])
}
