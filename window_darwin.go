//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

// Wails creates every NSWindow non-opaque with a clear background, which makes
// AppKit composite the window against whatever is behind it on every frame of a
// live resize. ClawHQ's window is a solid colour, so tell AppKit that: opaque,
// backed by the app's background colour, with the web view drawing its own
// background. Live resize then only has to scale the web content.
static void clawhqMakeOpaque(void* handle, int r, int g, int b) {
	NSWindow* win = (__bridge NSWindow*)handle;
	[win setOpaque:YES];
	[win setBackgroundColor:[NSColor colorWithSRGBRed:r/255.0 green:g/255.0 blue:b/255.0 alpha:1.0]];
	for (NSView* v in win.contentView.subviews) {
		if ([v isKindOfClass:[WKWebView class]]) {
			WKWebView* wv = (WKWebView*)v;
			@try { [wv setValue:@YES forKey:@"drawsBackground"]; } @catch (NSException* e) {}
			wv.wantsLayer = YES;
			wv.layer.backgroundColor = [NSColor colorWithSRGBRed:r/255.0 green:g/255.0 blue:b/255.0 alpha:1.0].CGColor;
		}
	}
}
*/
import "C"

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// tuneWindowForResize makes the main window opaque; see the C comment.
func tuneWindowForResize(w *application.WebviewWindow, r, g, b uint8) {
	application.InvokeSync(func() {
		h := w.NativeWindow()
		if h == nil {
			return
		}
		C.clawhqMakeOpaque(unsafe.Pointer(h), C.int(r), C.int(g), C.int(b))
	})
}
