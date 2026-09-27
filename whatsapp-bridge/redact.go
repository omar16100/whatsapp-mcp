package main

import (
	"fmt"
	"regexp"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// urlQueryPattern matches the query string of an http(s) URL inside free text.
var urlQueryPattern = regexp.MustCompile(`(https?://[^\s"'?]+)\?[^\s"']*`)

// redactURLs drops query strings from URLs embedded in s. WhatsApp media and
// upload URLs carry their authorization (oh/oe/auth tokens) in the query, and Go's
// url.Error includes the full request URL in its message.
func redactURLs(s string) string {
	return urlQueryPattern.ReplaceAllString(s, "$1?[redacted]")
}

// redactingLogger wraps a whatsmeow logger and strips URL query strings from every
// message, including those whatsmeow logs internally (e.g. media download retries
// print the transport error, which contains the signed CDN URL).
type redactingLogger struct {
	inner waLog.Logger
}

func newRedactingLogger(inner waLog.Logger) waLog.Logger {
	return &redactingLogger{inner: inner}
}

func (l *redactingLogger) clean(msg string, args []any) string {
	return redactURLs(fmt.Sprintf(msg, args...))
}

func (l *redactingLogger) Warnf(msg string, args ...any) { l.inner.Warnf("%s", l.clean(msg, args)) }
func (l *redactingLogger) Errorf(msg string, args ...any) {
	l.inner.Errorf("%s", l.clean(msg, args))
}
func (l *redactingLogger) Infof(msg string, args ...any)  { l.inner.Infof("%s", l.clean(msg, args)) }
func (l *redactingLogger) Debugf(msg string, args ...any) { l.inner.Debugf("%s", l.clean(msg, args)) }
func (l *redactingLogger) Sub(module string) waLog.Logger {
	return &redactingLogger{inner: l.inner.Sub(module)}
}
