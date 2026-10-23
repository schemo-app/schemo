#import <Cocoa/Cocoa.h>
#include <stdio.h>
#include "window_controls_darwin.h"

static int failures;

static void checkCenters(NSWindow* window, CGFloat height, const char* state) {
    NSView* content = window.contentView;
    for (NSNumber* kind in @[@(NSWindowCloseButton), @(NSWindowMiniaturizeButton), @(NSWindowZoomButton)]) {
        NSButton* button = [window standardWindowButton:kind.unsignedIntegerValue];
        NSRect frame = [button.superview convertRect:button.frame toView:content];
        CGFloat fromTop = content.isFlipped
            ? NSMidY(frame) - NSMinY(content.bounds)
            : NSMaxY(content.bounds) - NSMidY(frame);
        if (fabs(fromTop - height / 2.0) > 0.01) {
            fprintf(stderr, "%s: button %lu center = %.3f pt from top, want %.3f pt\n",
                    state, kind.unsignedIntegerValue, fromTop, height / 2.0);
            failures++;
        }
    }
}

@interface FlippedContent : NSView
@end
@implementation FlippedContent
- (BOOL)isFlipped { return YES; }
@end

int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        NSWindow* window = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 1000, 700)
            styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
                      NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable |
                      NSWindowStyleMaskFullSizeContentView
            backing:NSBackingStoreBuffered defer:NO];
        window.releasedWhenClosed = NO;
        window.titleVisibility = NSWindowTitleHidden;
        window.titlebarAppearsTransparent = YES;
        NSToolbar* toolbar = [[NSToolbar alloc] initWithIdentifier:@"controls-test"];
        window.toolbar = toolbar;
        [window.contentView.superview layoutSubtreeIfNeeded];
        schemoSetWindowHeaderHeight(window, 43.75);
        checkCenters(window, 43.75, "initial layout");

        // AppKit can reset one control after the window's resize callback has already run.
        NSButton* close = [window standardWindowButton:NSWindowCloseButton];
        NSPoint origin = close.frame.origin;
        origin.y -= 9;
        [close setFrameOrigin:origin];
        checkCenters(window, 43.75, "late button layout");

        NSView* container = close.superview;
        origin = container.frame.origin;
        origin.y += 5;
        [container setFrameOrigin:origin];
        checkCenters(window, 43.75, "late container layout");

        [window setContentSize:NSMakeSize(1200, 850)];
        [window.contentView.superview layoutSubtreeIfNeeded];
        checkCenters(window, 43.75, "resized window");

        for (NSNumber* height in @[@31.5, @57.125, @43.75]) {
            schemoSetWindowHeaderHeight(window, height.doubleValue);
            [window.contentView.superview layoutSubtreeIfNeeded];
            checkCenters(window, height.doubleValue, "changed interface scale");
        }

        NSNotificationCenter* center = NSNotificationCenter.defaultCenter;
        window.toolbar = nil;
        origin = close.frame.origin;
        origin.y -= 12;
        [close setFrameOrigin:origin];
        if (fabs(close.frame.origin.y - origin.y) > 0.01) {
            fprintf(stderr, "fullscreen transition: controls interfered with AppKit\n");
            failures++;
        }
        window.toolbar = toolbar;
        [center postNotificationName:NSWindowDidUpdateNotification object:window];
        checkCenters(window, 43.75, "returned from fullscreen");

        window.toolbar = nil;
        window.toolbar = toolbar;
        [window.contentView.superview layoutSubtreeIfNeeded];
        [center postNotificationName:NSWindowDidUpdateNotification object:window];
        checkCenters(window, 43.75, "restored toolbar");

        FlippedContent* flipped = [[FlippedContent alloc] initWithFrame:window.contentView.frame];
        window.contentView = flipped;
        [flipped release];
        [center postNotificationName:NSWindowDidUpdateNotification object:window];
        checkCenters(window, 43.75, "flipped content coordinates");

        [window close];
        [toolbar release];
        [window release];
    }
    if (failures == 0) {
        puts("Native controls remain centered after layout, resizing, scaling and fullscreen transitions.");
    }
    return failures ? 1 : 0;
}
