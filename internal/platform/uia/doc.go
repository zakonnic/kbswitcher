// Package uia is a small client of Microsoft UI Automation: the focused
// element, its class, bounds and state, and the text around its caret.
package uia

import "errors"

// errNoText means an element does not expose its text.
var errNoText = errors.New("the element does not expose its text")
