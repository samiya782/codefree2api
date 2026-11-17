package srd

type errString string

func (e errString) Error() string { return string(e) }

var errVersion = errString("version")
