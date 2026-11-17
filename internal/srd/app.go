package srd

// Kind selects the shipped client whose requests this process speaks.
type Kind string

const (
	KindPlugin Kind = "plugin"
	KindSpace  Kind = "space"
)

// App is one shipped client's host and credentials. The secret values live
// in cred.go and are not printed.
type App struct {
	Kind         Kind
	Host         string
	WS           string
	ClientID     string
	ClientSecret string
	Signing      string
}

func Plugin() App {
	return App{
		Kind:         KindPlugin,
		Host:         prodHost,
		WS:           prodWS,
		ClientID:     pluginClientID,
		ClientSecret: pluginClientSecret,
		Signing:      pluginSigning,
	}
}

func Space() App {
	return App{
		Kind:         KindSpace,
		Host:         prodHost,
		WS:           prodWS,
		ClientID:     spaceClientID,
		ClientSecret: spaceClientSecret,
		Signing:      spaceSigning,
	}
}
