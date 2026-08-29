package slogpretty

import "github.com/p1xray/sso/backend/pkg/logger/color"

type PrettyHandlerOption func(h *PrettyHandler)

func WithColor() PrettyHandlerOption {
	return func(h *PrettyHandler) {
		h.colorize = color.WithColorize
	}
}

func WithOutputEmptyAttrs() PrettyHandlerOption {
	return func(h *PrettyHandler) {
		h.outputEmptyAttrs = true
	}
}
