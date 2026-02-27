//go:build darwin

package input

/*
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices
#include <CoreGraphics/CoreGraphics.h>
#include <ApplicationServices/ApplicationServices.h>

static void postKeyEvent(CGKeyCode keyCode, bool keyDown, CGEventFlags flags) {
    CGEventRef event = CGEventCreateKeyboardEvent(NULL, keyCode, keyDown);
    if (flags != 0) {
        CGEventSetFlags(event, flags);
    }
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}

static void postUnicodeString(const UniChar *chars, UniCharCount length) {
    CGEventRef keyDown = CGEventCreateKeyboardEvent(NULL, 0, true);
    CGEventKeyboardSetUnicodeString(keyDown, length, chars);
    CGEventPost(kCGHIDEventTap, keyDown);
    CFRelease(keyDown);

    CGEventRef keyUp = CGEventCreateKeyboardEvent(NULL, 0, false);
    CGEventKeyboardSetUnicodeString(keyUp, length, chars);
    CGEventPost(kCGHIDEventTap, keyUp);
    CFRelease(keyUp);
}

static void postMouseEvent(CGEventType type, CGFloat x, CGFloat y, CGMouseButton button) {
    CGPoint point = CGPointMake(x, y);
    CGEventRef event = CGEventCreateMouseEvent(NULL, type, point, button);
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}

static CGPoint getMousePosition(void) {
    CGEventRef event = CGEventCreate(NULL);
    CGPoint point = CGEventGetLocation(event);
    CFRelease(event);
    return point;
}

static void postScrollEvent(int32_t deltaY, int32_t deltaX) {
    CGEventRef event = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitLine, 2, deltaY, deltaX);
    CGEventPost(kCGHIDEventTap, event);
    CFRelease(event);
}

static bool checkAccessibility(void) {
    return AXIsProcessTrusted();
}
*/
import "C"

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"
)

// macOS virtual key codes (from Events.h / HIToolbox)
var keyCodeMap = map[string]C.CGKeyCode{
	// Letters
	"a": 0x00, "s": 0x01, "d": 0x02, "f": 0x03, "h": 0x04, "g": 0x05,
	"z": 0x06, "x": 0x07, "c": 0x08, "v": 0x09, "b": 0x0B, "q": 0x0C,
	"w": 0x0D, "e": 0x0E, "r": 0x0F, "y": 0x10, "t": 0x11,
	"i": 0x22, "j": 0x26, "k": 0x28, "l": 0x25, "m": 0x2E,
	"n": 0x2D, "o": 0x1F, "p": 0x23, "u": 0x20,

	// Numbers
	"1": 0x12, "2": 0x13, "3": 0x14, "4": 0x15, "5": 0x17, "6": 0x16,
	"7": 0x1A, "8": 0x1C, "9": 0x19, "0": 0x1D,

	// Punctuation / symbols
	"equal": 0x18, "minus": 0x1B,
	"rightbracket": 0x1E, "leftbracket": 0x21,
	"apostrophe": 0x27, "semicolon": 0x29, "backslash": 0x2A,
	"comma": 0x2B, "slash": 0x2C, "period": 0x2F, "grave": 0x32,

	// Special keys
	"return": 0x24, "enter": 0x24, "tab": 0x30, "space": 0x31,
	"backspace": 0x33, "escape": 0x35, "delete": 0x75,
	"capslock": 0x39,

	// Arrow keys
	"left": 0x7B, "right": 0x7C, "down": 0x7D, "up": 0x7E,

	// Navigation
	"home": 0x73, "end": 0x77,
	"pageup": 0x74, "page_up": 0x74,
	"pagedown": 0x79, "page_down": 0x79,

	// Function keys
	"f1": 0x7A, "f2": 0x78, "f3": 0x63, "f4": 0x76,
	"f5": 0x60, "f6": 0x61, "f7": 0x62, "f8": 0x64,
	"f9": 0x65, "f10": 0x6D, "f11": 0x67, "f12": 0x6F,

	// Modifier keys (for standalone keydown/keyup)
	"shift": 0x38, "leftshift": 0x38, "rightshift": 0x3C, "shift_l": 0x38, "shift_r": 0x3C,
	"control": 0x3B, "ctrl": 0x3B, "leftcontrol": 0x3B, "rightcontrol": 0x3E, "control_l": 0x3B, "control_r": 0x3E,
	"alt": 0x3A, "option": 0x3A, "leftoption": 0x3A, "rightoption": 0x3D, "alt_l": 0x3A, "alt_r": 0x3D,
	"super": 0x37, "command": 0x37, "leftcommand": 0x37, "rightcommand": 0x36, "super_l": 0x37, "super_r": 0x36,
	"meta": 0x37, "meta_l": 0x37, "meta_r": 0x36,
}

// Modifier name to CGEventFlags mapping
var modifierFlagMap = map[string]C.CGEventFlags{
	"shift":   C.kCGEventFlagMaskShift,
	"control": C.kCGEventFlagMaskControl,
	"ctrl":    C.kCGEventFlagMaskControl,
	"alt":     C.kCGEventFlagMaskAlternate,
	"option":  C.kCGEventFlagMaskAlternate,
	"super":   C.kCGEventFlagMaskCommand,
	"command": C.kCGEventFlagMaskCommand,
	"meta":    C.kCGEventFlagMaskCommand,
}

type cgController struct{}

func New() (Controller, error) {
	if !bool(C.checkAccessibility()) {
		return nil, fmt.Errorf("accessibility permissions not granted: enable in System Settings > Privacy & Security > Accessibility")
	}
	return &cgController{}, nil
}

func (c *cgController) ExecuteCommands(ctx context.Context, commands []Command) error {
	log.Printf("cgController.ExecuteCommands: executing %d commands", len(commands))

	for i, cmd := range commands {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		log.Printf("cgController.ExecuteCommands: [%d/%d] %s %s", i+1, len(commands), cmd.Action, cmd.Args)

		switch cmd.Action {
		case "sleep":
			ms, err := strconv.Atoi(strings.TrimSpace(cmd.Args))
			if err != nil {
				return fmt.Errorf("invalid sleep duration: %w", err)
			}
			time.Sleep(time.Duration(ms) * time.Millisecond)

		case "key":
			if err := c.doKey(cmd.Args); err != nil {
				return fmt.Errorf("key %q: %w", cmd.Args, err)
			}

		case "keydown":
			if err := c.doKeyDown(cmd.Args); err != nil {
				return fmt.Errorf("keydown %q: %w", cmd.Args, err)
			}

		case "keyup":
			if err := c.doKeyUp(cmd.Args); err != nil {
				return fmt.Errorf("keyup %q: %w", cmd.Args, err)
			}

		case "type":
			c.doType(cmd.Args)

		case "click":
			c.doClick(cmd.Args)

		case "buttondown":
			c.doButtonDown(cmd.Args)

		case "buttonup":
			c.doButtonUp(cmd.Args)

		case "mouseto":
			if err := c.doMouseTo(cmd.Args); err != nil {
				return fmt.Errorf("mouseto %q: %w", cmd.Args, err)
			}

		case "mousemove":
			if err := c.doMouseMove(cmd.Args); err != nil {
				return fmt.Errorf("mousemove %q: %w", cmd.Args, err)
			}

		case "wheel":
			if err := c.doWheel(cmd.Args); err != nil {
				return fmt.Errorf("wheel %q: %w", cmd.Args, err)
			}

		case "hwheel":
			if err := c.doHWheel(cmd.Args); err != nil {
				return fmt.Errorf("hwheel %q: %w", cmd.Args, err)
			}

		case "keydelay", "keyhold", "typedelay", "typehold":
			log.Printf("cgController.ExecuteCommands: skipping timing command %s (not supported on macOS)", cmd.Action)

		default:
			log.Printf("cgController.ExecuteCommands: unknown action %q", cmd.Action)
		}
	}

	log.Println("cgController.ExecuteCommands: completed successfully")
	return nil
}

func parseKeySpec(spec string) (keyCode C.CGKeyCode, flags C.CGEventFlags, err error) {
	parts := strings.Split(strings.ToLower(spec), "+")
	keyName := parts[len(parts)-1]

	for _, part := range parts[:len(parts)-1] {
		if flag, ok := modifierFlagMap[part]; ok {
			flags |= flag
		} else {
			return 0, 0, fmt.Errorf("unknown modifier %q", part)
		}
	}

	code, ok := keyCodeMap[keyName]
	if !ok {
		return 0, 0, fmt.Errorf("unknown key %q", keyName)
	}

	return code, flags, nil
}

func (c *cgController) doKey(spec string) error {
	keyCode, flags, err := parseKeySpec(spec)
	if err != nil {
		return err
	}
	C.postKeyEvent(keyCode, C.bool(true), flags)
	time.Sleep(5 * time.Millisecond)
	C.postKeyEvent(keyCode, C.bool(false), flags)
	time.Sleep(5 * time.Millisecond)
	return nil
}

func (c *cgController) doKeyDown(spec string) error {
	keyCode, flags, err := parseKeySpec(spec)
	if err != nil {
		return err
	}
	C.postKeyEvent(keyCode, C.bool(true), flags)
	return nil
}

func (c *cgController) doKeyUp(spec string) error {
	keyCode, flags, err := parseKeySpec(spec)
	if err != nil {
		return err
	}
	C.postKeyEvent(keyCode, C.bool(false), flags)
	return nil
}

func (c *cgController) doType(text string) {
	runes := []rune(text)
	if len(runes) == 0 {
		return
	}

	const chunkSize = 20
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunk := runes[i:end]
		uniChars := utf16.Encode(chunk)
		C.postUnicodeString((*C.UniChar)(unsafe.Pointer(&uniChars[0])), C.UniCharCount(len(uniChars)))
		time.Sleep(5 * time.Millisecond)
	}
}

func parseMouseButton(name string) (C.CGMouseButton, C.CGEventType, C.CGEventType) {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "right":
		return C.kCGMouseButtonRight, C.kCGEventRightMouseDown, C.kCGEventRightMouseUp
	case "middle":
		return C.kCGMouseButtonCenter, C.kCGEventOtherMouseDown, C.kCGEventOtherMouseUp
	default: // "left" or unrecognized
		return C.kCGMouseButtonLeft, C.kCGEventLeftMouseDown, C.kCGEventLeftMouseUp
	}
}

func (c *cgController) doClick(args string) {
	pos := C.getMousePosition()
	button, downType, upType := parseMouseButton(args)
	C.postMouseEvent(downType, pos.x, pos.y, button)
	time.Sleep(10 * time.Millisecond)
	C.postMouseEvent(upType, pos.x, pos.y, button)
	time.Sleep(5 * time.Millisecond)
}

func (c *cgController) doButtonDown(args string) {
	pos := C.getMousePosition()
	button, downType, _ := parseMouseButton(args)
	C.postMouseEvent(downType, pos.x, pos.y, button)
}

func (c *cgController) doButtonUp(args string) {
	pos := C.getMousePosition()
	button, _, upType := parseMouseButton(args)
	C.postMouseEvent(upType, pos.x, pos.y, button)
}

func (c *cgController) doMouseTo(args string) error {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		return fmt.Errorf("mouseto requires 2 arguments: x y")
	}
	x, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return fmt.Errorf("invalid x: %w", err)
	}
	y, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return fmt.Errorf("invalid y: %w", err)
	}
	C.postMouseEvent(C.kCGEventMouseMoved, C.CGFloat(x), C.CGFloat(y), C.kCGMouseButtonLeft)
	return nil
}

func (c *cgController) doMouseMove(args string) error {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		return fmt.Errorf("mousemove requires 2 arguments: dx dy")
	}
	dx, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return fmt.Errorf("invalid dx: %w", err)
	}
	dy, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return fmt.Errorf("invalid dy: %w", err)
	}
	pos := C.getMousePosition()
	newX := float64(pos.x) + dx
	newY := float64(pos.y) + dy
	C.postMouseEvent(C.kCGEventMouseMoved, C.CGFloat(newX), C.CGFloat(newY), C.kCGMouseButtonLeft)
	return nil
}

func (c *cgController) doWheel(args string) error {
	n, err := strconv.Atoi(strings.TrimSpace(args))
	if err != nil {
		return fmt.Errorf("invalid wheel value: %w", err)
	}
	C.postScrollEvent(C.int32_t(n), 0)
	return nil
}

func (c *cgController) doHWheel(args string) error {
	n, err := strconv.Atoi(strings.TrimSpace(args))
	if err != nil {
		return fmt.Errorf("invalid hwheel value: %w", err)
	}
	C.postScrollEvent(0, C.int32_t(n))
	return nil
}

func (c *cgController) TypeText(ctx context.Context, text string) error {
	return c.ExecuteCommands(ctx, []Command{{Action: "type", Args: text}})
}

func (c *cgController) Close() error {
	return nil
}
