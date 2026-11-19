package core

import (
	"context"
	"errors"

	"imageworkbench/internal/i18n"
)

// User-facing backend messages come from the catalogs in internal/i18n/messages
// (see i18n.T). Write them in the interface language current at that moment;
// stored text keeps it (ADR 0004).
//
//	s.msg(key, "name", v)       text, inside Service methods
//	s.fail(key, "name", v)      the same as an error
//	msgCtx(ctx, key, ...)       text in the language carried by ctx (s.langCtx)
//	failCtx(ctx, key, ...)      the same as an error
//
// Upstream error text goes in as a placeholder value, verbatim.

func (s *Service) msg(key string, args ...any) string { return i18n.T(s.Language(), key, args...) }

func (s *Service) fail(key string, args ...any) error { return errors.New(s.msg(key, args...)) }

// langCtx carries the current interface language into ctx for code without a
// Service (the provider, output downloads, readers of LLM replies).
func (s *Service) langCtx(ctx context.Context) context.Context {
	return i18n.WithLanguage(ctx, s.Language())
}

func msgCtx(ctx context.Context, key string, args ...any) string {
	return i18n.T(i18n.FromContext(ctx), key, args...)
}

func failCtx(ctx context.Context, key string, args ...any) error {
	return errors.New(msgCtx(ctx, key, args...))
}

// storeLang is the Store's language source, for log lines the Store writes itself.
func (st *Store) language() string {
	if st.lang != nil {
		return st.lang()
	}
	return i18n.Default
}

func (st *Store) msg(key string, args ...any) string { return i18n.T(st.language(), key, args...) }

// text writes a deferred message (i18n.Message) in the current language.
func (s *Service) text(m i18n.Message) string { return m.In(s.Language()) }

// errText is an error's text in the current language: catalog errors from
// packages without a language (update, license) are put into it, any other
// error keeps its own text.
func (s *Service) errText(e error) string { return i18n.Text(e, s.Language()) }

// localErr is e with its text in the current language; errors.Is still sees e.
func (s *Service) localErr(e error) error {
	if e == nil {
		return nil
	}
	return localizedError{s.errText(e), e}
}

type localizedError struct {
	text  string
	cause error
}

func (e localizedError) Error() string { return e.text }
func (e localizedError) Unwrap() error { return e.cause }

// LocalError is e with its text in the current interface language, for the
// app layer (dialogs, folders) that returns catalog errors (i18n.Fail).
func (s *Service) LocalError(e error) error { return s.localErr(e) }
