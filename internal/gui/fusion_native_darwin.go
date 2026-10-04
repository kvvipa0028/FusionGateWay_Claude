//go:build fusion && !nogui && darwin

package gui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>
#include <stdlib.h>

static BOOL fusionNavigationURLAllowed(NSURL *u) {
 return [u.absoluteString isEqualToString:@"wails://localhost/fusion/"] ||
  [u.absoluteString isEqualToString:@"wails://localhost/fusion/index.html"];
}
static int probeFusionNavigation(const char *uri, int main) {
 return main && fusionNavigationURLAllowed([NSURL URLWithString:[NSString stringWithUTF8String:uri]]);
}

// Restrict only the delegate instance of the owned Fusion window. Inherit
// Wails' scheme/message/event methods; other windows keep their own class.
static void fusionNavigation(id self, SEL selector, WKWebView *web,
 WKNavigationAction *action, void (^decision)(WKNavigationActionPolicy)) {
 NSURL *u = action.request.URL;
 BOOL allowed = action.targetFrame != nil && action.targetFrame.mainFrame &&
  fusionNavigationURLAllowed(u);
 decision(allowed ? WKNavigationActionPolicyAllow : WKNavigationActionPolicyCancel);
}

static int restrictFusionNavigation(void *window) {
 NSWindow *win = (NSWindow *)window;
 if (win == nil || ![win respondsToSelector:@selector(webView)]) return 0;
 id web = [win performSelector:@selector(webView)];
 if (![web isKindOfClass:[WKWebView class]]) return 0;
 id delegate = ((WKWebView *)web).navigationDelegate;
 if (delegate == nil) return 0;
 Class base = object_getClass(delegate);
 Class restricted = objc_getClass("FusionStageNavigationDelegate");
 if (restricted == Nil) {
  restricted = objc_allocateClassPair(base, "FusionStageNavigationDelegate", 0);
  if (restricted == Nil || !class_addMethod(restricted,
   @selector(webView:decidePolicyForNavigationAction:decisionHandler:),
   (IMP)fusionNavigation, "v@:@@@?")) return 0;
  objc_registerClassPair(restricted);
 }
 if (class_getSuperclass(restricted) != base) return 0;
 object_setClass(delegate, restricted);
 return 1;
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/yetone/magpie/internal/fusion/bootstrap"
	"github.com/yetone/magpie/internal/fusion/isolation"
)

func nativeStageNavigationAllowed(uri string, mainFrame bool) bool {
	raw := C.CString(uri)
	defer C.free(unsafe.Pointer(raw))
	main := 0
	if mainFrame {
		main = 1
	}
	return C.probeFusionNavigation(raw, C.int(main)) == 1
}

// RunFusion owns one draft-control host and one Native stage window. It does
// not start the legacy gateway, install Runtime authority or register services.
func RunFusion(parent context.Context, source, root string, out io.Writer) error {
	if !isolation.Development || parent == nil || out == nil {
		return bootstrap.ErrControlHost
	}
	control, e := bootstrap.OpenControl(source, root, "127.0.0.1:0")
	if e != nil {
		return bootstrap.ErrControlHost
	}
	bridge, e := bootstrap.NewNativeStageBridge(control)
	if e != nil {
		control.Close()
		return bootstrap.ErrControlHost
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan error, 1)
	go func() { done <- control.Serve(ctx) }()
	var once sync.Once
	var shutdownErr error
	shutdown := func() {
		once.Do(func() {
			bridge.Close()
			cancel()
			shutdownErr = control.Close()
			if e := <-done; e != nil {
				shutdownErr = e
			}
		})
	}
	defer shutdown()
	app := application.New(application.Options{
		Name: isolation.Title, Description: "Fusion stage configuration", Icon: appIconFor(),
		Mac:          application.MacOptions{ActivationPolicy: application.ActivationPolicyRegular},
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Assets:       application.AssetOptions{Handler: bridge, Middleware: func(_ http.Handler) http.Handler { return bridge }, DisableLogging: true},
		OnShutdown:   shutdown,
		ErrorHandler: func(error) { cancel() },
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "fusion-stage-editor", Title: isolation.Title + " · 阶段模型配置", URL: "about:blank",
		Width: 1060, Height: 820, MinWidth: 760, MinHeight: 600,
		DevToolsEnabled: false, DefaultContextMenuDisabled: true,
	})
	window.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) { go app.Quit() })
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if bridge.BindNativeWindow(window.ID()) != nil || C.restrictFusionNavigation(window.NativeWindow()) != 1 {
			cancel()
			return
		}
		if json.NewEncoder(out).Encode(map[string]any{"product": "fusion-gateway", "control_address": "http://" + control.Addr(), "mode": "native-stage-editor", "execution_enabled": false, "jev": "off"}) != nil {
			cancel()
			return
		}
		window.SetURL("/fusion/")
	})
	go func() { <-ctx.Done(); app.Quit() }()
	if e = app.Run(); e != nil {
		return bootstrap.ErrControlHost
	}
	shutdown()
	if shutdownErr != nil {
		return bootstrap.ErrControlHost
	}
	return nil
}
