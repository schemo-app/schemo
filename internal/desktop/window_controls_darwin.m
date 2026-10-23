//go:build darwin

#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>
#import "window_controls_darwin.h"

// AppKit can move the buttons after a resize callback, when it lays out the toolbar.
// Observe their native frames so that later layout passes preserve the web header's center.
@interface SchemoWindowControls : NSObject {
    NSWindow* _window;
    NSHashTable<NSView*>* _observedViews;
    BOOL _aligning;
}
@property(nonatomic) CGFloat headerHeight;
- (instancetype)initWithWindow:(NSWindow*)window;
- (void)align;
@end

@implementation SchemoWindowControls

- (instancetype)initWithWindow:(NSWindow*)window {
    self = [super init];
    if (self == nil) {
        return nil;
    }
    _window = window;
    _observedViews = [[NSHashTable weakObjectsHashTable] retain];
    NSNotificationCenter* center = NSNotificationCenter.defaultCenter;
    for (NSNotificationName name in @[NSWindowDidResizeNotification, NSWindowDidUpdateNotification]) {
        [center addObserver:self selector:@selector(geometryChanged:) name:name object:window];
    }
    [center addObserver:self selector:@selector(windowClosed:)
                   name:NSWindowWillCloseNotification object:window];
    return self;
}

- (void)dealloc {
    [NSNotificationCenter.defaultCenter removeObserver:self];
    [_observedViews release];
    [super dealloc];
}

- (void)observeView:(NSView*)view {
    if ([_observedViews containsObject:view]) {
        return;
    }
    [_observedViews addObject:view];
    [NSNotificationCenter.defaultCenter addObserver:self selector:@selector(geometryChanged:)
                                               name:NSViewFrameDidChangeNotification object:view];
    view.postsFrameChangedNotifications = YES;
}

- (void)geometryChanged:(NSNotification*)notification {
    [self align];
}

- (void)windowClosed:(NSNotification*)notification {
    [NSNotificationCenter.defaultCenter removeObserver:self];
    [_observedViews removeAllObjects];
    _window = nil;
}

- (void)align {
    if (_aligning || _window == nil || _window.toolbar == nil ||
        (_window.styleMask & NSWindowStyleMaskFullScreen)) {
        return;
    }
    NSView* content = _window.contentView;
    if (content == nil) {
        return;
    }
    _aligning = YES;
    CGFloat centerY = content.isFlipped
        ? NSMinY(content.bounds) + self.headerHeight / 2.0
        : NSMaxY(content.bounds) - self.headerHeight / 2.0;
    for (NSNumber* kind in @[@(NSWindowCloseButton), @(NSWindowMiniaturizeButton), @(NSWindowZoomButton)]) {
        NSButton* button = [_window standardWindowButton:kind.unsignedIntegerValue];
        if (button == nil || button.superview == nil) {
            continue;
        }
        for (NSView* view = button; view != nil; view = view.superview) {
            [self observeView:view];
        }
        [self observeView:content];
        NSPoint center = [button.superview convertPoint:NSMakePoint(0, centerY) fromView:content];
        NSRect frame = button.frame;
        frame.origin.y = center.y - NSHeight(frame) / 2.0;
        if (!NSEqualRects(frame, button.frame)) {
            [button setFrame:frame];
        }
    }
    _aligning = NO;
}

@end

void schemoSetWindowHeaderHeight(void* nsWindow, double height) {
    NSWindow* window = (NSWindow*)nsWindow;
    if (window == nil) {
        return;
    }
    static const void* controlsKey = &controlsKey;
    SchemoWindowControls* controls = objc_getAssociatedObject(window, controlsKey);
    if (controls == nil) {
        controls = [[SchemoWindowControls alloc] initWithWindow:window];
        objc_setAssociatedObject(window, controlsKey, controls, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
        [controls release];
    }
    controls.headerHeight = height;
    [controls align];
}
