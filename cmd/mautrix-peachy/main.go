package main

import (
	"maunium.net/go/mautrix/bridgev2/matrix/mxmain"

	"github.com/collinjackson/mautrix-peachy/pkg/connector"
)

var (
	Tag       = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

var m = mxmain.BridgeMain{
	Name:        "mautrix-peachy",
	URL:         "https://github.com/collinjackson/mautrix-peachy",
	Description: "A Matrix-Peachy puppeting bridge.",
	Version:     "0.1.0",

	Connector: &connector.PeachyConnector{},
}

func main() {
	m.InitVersion(Tag, Commit, BuildTime)
	m.Run()
}
