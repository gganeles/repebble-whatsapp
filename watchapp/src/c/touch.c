#include "touch.h"

#if defined(_PBL_API_EXISTS_tap_recognizer_create) && defined(_PBL_API_EXISTS_window_set_touch_bridge_disabled)

#define MAX_BINDINGS 4

// One per window with touch: recognizer callbacks don't carry user data, so look them up here.
typedef struct {
  Window *window;
  Recognizer *tap;
  Recognizer *pan;
  ScrollLayer *scroll;
  MenuLayer *menu;
  TouchRowCount rows;
  TouchRowHeight height;
  TouchTapHandler on_tap;
  int16_t base;  // content offset when the drag started
} Binding;

static Binding s_bindings[MAX_BINDINGS];

static Binding *find(const Recognizer *r) {
  for (int i = 0; i < MAX_BINDINGS; i++) {
    if (s_bindings[i].window && (s_bindings[i].tap == r || s_bindings[i].pan == r)) {
      return &s_bindings[i];
    }
  }
  return NULL;
}

static void noop_handler(const TouchEvent *event, void *context) {
}

void touch_init(void) {
  touch_service_subscribe(noop_handler, NULL);
}

void touch_deinit(void) {
  touch_service_unsubscribe();
}

static void tap_cb(const Recognizer *recognizer, RecognizerEvent event) {
  Binding *b = find(recognizer);
  if (b && b->on_tap && event == RecognizerEvent_Completed) {
    b->on_tap(tap_recognizer_get_tap_point(recognizer));
  }
}

static void set_offset(ScrollLayer *scroll, int16_t y) {
  GRect frame = layer_get_frame(scroll_layer_get_layer(scroll));
  int16_t min = frame.size.h - scroll_layer_get_content_size(scroll).h;
  if (y < min) {
    y = min;
  }
  if (y > 0) {
    y = 0;
  }
  scroll_layer_set_content_offset(scroll, GPoint(0, y), false);
}

static void select_visible_row(Binding *b) {
  GRect frame = layer_get_frame(menu_layer_get_layer(b->menu));
  int row = touch_menu_row_at(b->menu, GPoint(frame.size.w / 2, frame.origin.y + frame.size.h / 2), b->rows, b->height);
  if (row >= 0 && row != menu_layer_get_selected_index(b->menu).row) {
    menu_layer_set_selected_index(b->menu, MenuIndex(0, row), MenuRowAlignNone, false);
  }
}

static void pan_cb(const Recognizer *recognizer, RecognizerEvent event) {
  Binding *b = find(recognizer);
  if (!b || !b->scroll) {
    return;
  }
  switch (event) {
    case RecognizerEvent_Started:
      b->base = scroll_layer_get_content_offset(b->scroll).y;
      break;
    case RecognizerEvent_Updated:
      set_offset(b->scroll, b->base + pan_recognizer_get_delta_since_start(recognizer).y);
      break;
    case RecognizerEvent_Completed:
      if (b->menu) {
        select_visible_row(b);
      }
      break;
    case RecognizerEvent_Cancelled:
      set_offset(b->scroll, b->base);
      break;
  }
}

static Binding *attach(Window *window, ScrollLayer *scroll, MenuLayer *menu, TouchTapHandler on_tap) {
  Binding *b = NULL;
  for (int i = 0; i < MAX_BINDINGS && !b; i++) {
    if (!s_bindings[i].window) {
      b = &s_bindings[i];
    }
  }
  if (!b) {
    return NULL;
  }
  *b = (Binding) { .window = window, .scroll = scroll, .menu = menu, .on_tap = on_tap };
  // Take touch input ourselves instead of the system's button emulation.
  window_set_touch_bridge_disabled(window, true);
  if (on_tap) {
    b->tap = tap_recognizer_create(tap_cb, NULL);
    window_attach_recognizer(window, b->tap);
  }
  if (scroll) {
    b->pan = pan_recognizer_create(pan_cb, NULL, PanAxis_Vertical);
    window_attach_recognizer(window, b->pan);
  }
  return b;
}

void touch_attach(Window *window, ScrollLayer *scroll, TouchTapHandler on_tap) {
  attach(window, scroll, NULL, on_tap);
}

void touch_attach_menu(Window *window, MenuLayer *menu, TouchRowCount rows, TouchRowHeight height,
                       TouchTapHandler on_tap) {
  Binding *b = attach(window, menu_layer_get_scroll_layer(menu), menu, on_tap);
  if (b) {
    b->rows = rows;
    b->height = height;
  }
}

void touch_detach(Window *window) {
  for (int i = 0; i < MAX_BINDINGS; i++) {
    if (s_bindings[i].window == window) {
      // The window owns its recognizers and destroys them itself.
      s_bindings[i] = (Binding) { 0 };
    }
  }
}

int touch_menu_row_at(MenuLayer *menu, GPoint point, TouchRowCount rows, TouchRowHeight height) {
  GRect frame = layer_get_frame(menu_layer_get_layer(menu));
  int16_t offset = scroll_layer_get_content_offset(menu_layer_get_scroll_layer(menu)).y;
  int y = point.y - frame.origin.y - offset;
  if (point.y < frame.origin.y || y < 0) {
    return -1;
  }
  int top = 0;
  uint16_t count = rows(menu, 0, NULL);
  for (uint16_t row = 0; row < count; row++) {
    MenuIndex index = MenuIndex(0, row);
    top += height(menu, &index, NULL);
    if (y < top) {
      return row;
    }
  }
  return -1;
}

#else

void touch_init(void) {}
void touch_deinit(void) {}
void touch_attach(Window *window, ScrollLayer *scroll, TouchTapHandler on_tap) {}
void touch_attach_menu(Window *window, MenuLayer *menu, TouchRowCount rows, TouchRowHeight height,
                       TouchTapHandler on_tap) {}
void touch_detach(Window *window) {}
int touch_menu_row_at(MenuLayer *menu, GPoint point, TouchRowCount rows, TouchRowHeight height) { return -1; }

#endif
