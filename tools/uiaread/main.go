//go:build windows

// Command uiaread prints what UI Automation reports about the focused text
// field, for checking which applications kbswitcher can read without the
// clipboard. With -text=false the text itself is left out, only its length
// shown.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/zakonnic/kbswitcher/internal/platform/field"
	"github.com/zakonnic/kbswitcher/internal/platform/input"
	"github.com/zakonnic/kbswitcher/internal/platform/uia"
)

type report struct {
	Class, Framework                 string
	ControlType                      int32
	PID                              uint32
	Password, Editable, HasText      bool
	Before, Selected, After, Error   string `json:",omitempty"`
	BeforeLen, SelectedLen, AfterLen int
	FocusMillis, ReadMillis          float64
	Parents                          []string `json:",omitempty"`
	Bounds                           uia.Rect
}

func main() {
	delay := flag.Duration("delay", 0, "wait this long first")
	showText := flag.Bool("text", true, "print the text, not only its length")
	parents := flag.Int("parents", 0, "list the classes and control types of this many ancestors")
	lag := flag.Int("lag", 0, "type a character this many times and measure how long until UI Automation shows it")
	whole := flag.Bool("field", false, "print what kbswitcher's field reader returns, classic edit controls included")
	flag.Parse()
	time.Sleep(*delay)
	if *whole {
		a, err := field.Read(40, 40)
		fmt.Println("before:", strconv.Quote(a.Before), "selected:", strconv.Quote(a.Selected),
			"after:", strconv.Quote(a.After), "error:", err)
		return
	}
	if *lag > 0 {
		measureLag(*lag)
		return
	}

	var r report
	err := uia.With(func(c *uia.Client) error {
		start := time.Now()
		e, err := c.Focused()
		if err != nil {
			return err
		}
		defer e.Release()
		r.FocusMillis = ms(time.Since(start))
		r.Class, r.Framework, r.ControlType, r.PID = e.Class(), e.Framework(), e.ControlType(), e.ProcessID()
		r.Password, r.Editable, r.Bounds = e.Password(), e.Editable(), e.Bounds()
		at := e
		for range *parents {
			up := c.Parent(at)
			if up == nil {
				break
			}
			if at != e {
				at.Release()
			}
			at = up
			r.Parents = append(r.Parents, fmt.Sprintf("%s/%d", at.Class(), at.ControlType()))
		}
		if at != e {
			at.Release()
		}
		start = time.Now()
		t, err := e.Text()
		if err != nil {
			return err
		}
		defer t.Release()
		r.HasText = true
		var a field.Around
		a.Before, a.Selected, a.After, err = t.Around(200, 40)
		r.ReadMillis = ms(time.Since(start))
		if err != nil {
			return err
		}
		r.BeforeLen, r.SelectedLen, r.AfterLen = utf8.RuneCountInString(a.Before), utf8.RuneCountInString(a.Selected), utf8.RuneCountInString(a.After)
		if *showText {
			r.Before, r.Selected, r.After = a.Before, a.Selected, a.After
		}
		return nil
	})
	if err != nil {
		r.Error = err.Error()
	}
	out, _ := json.Marshal(r)
	fmt.Fprintln(os.Stdout, string(out))
}

// measureLag types x, waits for the focused field to show it, and erases it
// again, n times; then presses Left and waits for the caret to show it moved.
func measureLag(n int) {
	keys, err := input.Open()
	if err != nil {
		panic(err)
	}
	err = uia.With(func(c *uia.Client) error {
		e, err := c.Focused()
		if err != nil {
			return err
		}
		defer e.Release()
		t, err := e.Text()
		if err != nil {
			return err
		}
		defer t.Release()
		read := func() field.Around {
			var a field.Around
			a.Before, a.Selected, a.After, _ = t.Around(50, 50)
			return a
		}
		await := func(changed func(field.Around) bool) time.Duration {
			start := time.Now()
			for time.Since(start) < 2*time.Second {
				if changed(read()) {
					return time.Since(start)
				}
				time.Sleep(500 * time.Microsecond)
			}
			return -1
		}
		var typed, moved []time.Duration
		for range n {
			was := read()
			check(keys.Retype(false, 0, "x"))
			typed = append(typed, await(func(a field.Around) bool { return a.Before != was.Before }))
			check(keys.Retype(false, 1, ""))
			await(func(a field.Around) bool { return a.Before == was.Before })
			time.Sleep(50 * time.Millisecond)
			check(keys.Nudge(false))
			moved = append(moved, await(func(a field.Around) bool { return a.After != was.After }))
			check(keys.Nudge(true))
			await(func(a field.Around) bool { return a.After == was.After })
			time.Sleep(50 * time.Millisecond)
		}
		fmt.Println("typed:", summary(typed))
		fmt.Println("moved:", summary(moved))
		return nil
	})
	check(err)
}

func summary(ds []time.Duration) string {
	slices.Sort(ds)
	return fmt.Sprintf("min %v median %v max %v (of %d; -1 means never)", ds[0].Round(100*time.Microsecond),
		ds[len(ds)/2].Round(100*time.Microsecond), ds[len(ds)-1].Round(100*time.Microsecond), len(ds))
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
