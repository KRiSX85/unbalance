package domain

import "github.com/cskr/pubsub"

type Context struct {
	Config

	Port    string
	LogsDir string
	DataDir string
	Paths   RuntimePaths
	Hub     *pubsub.PubSub
}
