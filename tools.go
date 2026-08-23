//go:build tools

package main

import (
	_ "github.com/gotd/td/session"
	_ "github.com/gotd/td/telegram"
	_ "github.com/gotd/td/telegram/auth"
	_ "github.com/gotd/td/telegram/query/channels/participants"
	_ "github.com/gotd/td/tg"
)
