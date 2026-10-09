// Touchscreen input on watches that have one (Pebble Time 2). No-ops elsewhere.
#pragma once
#include <pebble.h>

typedef void (*TouchTapHandler)(GPoint point);

// Turns the touch sensor on for the app's lifetime.
void touch_init(void);
void touch_deinit(void);

// Taps call on_tap with the point in window coordinates; vertical drags scroll `scroll` (may be NULL).
// Call from window load; touch_detach from window unload.
void touch_attach(Window *window, ScrollLayer *scroll, TouchTapHandler on_tap);
void touch_detach(Window *window);

// Single-section MenuLayer callbacks, used to find rows by position.
typedef uint16_t (*TouchRowCount)(MenuLayer *menu, uint16_t section, void *ctx);
typedef int16_t (*TouchRowHeight)(MenuLayer *menu, MenuIndex *index, void *ctx);

// Like touch_attach for a MenuLayer. After a drag the row in the middle of the view is selected,
// so selection_changed (paging) still fires and buttons continue from where the finger left off.
void touch_attach_menu(Window *window, MenuLayer *menu, TouchRowCount rows, TouchRowHeight height,
                       TouchTapHandler on_tap);

// The menu row under a point in window coordinates, or -1.
int touch_menu_row_at(MenuLayer *menu, GPoint point, TouchRowCount rows, TouchRowHeight height);
