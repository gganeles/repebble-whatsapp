#include "touch.h"

#if defined(_PBL_API_EXISTS_touch_service_subscribe)

#define MAX_BINDINGS 4
// Movement beyond this many pixels turns a touch into a drag instead of a tap.
#define DRAG_THRESHOLD 8

// One per window with touch. Events go to the binding of the top window.
typedef struct {
  Window *window;
  ScrollLayer *scroll;
  MenuLayer *menu;
  TouchRowCount rows;
  TouchRowHeight height;
  TouchTapHandler on_tap;
} Binding;

static Binding s_bindings[MAX_BINDINGS];

// The gesture in progress.
static Binding *s_active;
static GPoint s_start;
static GPoint s_last;
static int16_t s_base;  // content offset at touchdown
static bool s_dragging;

static Binding *top_binding(void) {
  Window *top = window_stack_get_top_window();
  for (int i = 0; i < MAX_BINDINGS; i++) {
    if (top && s_bindings[i].window == top) {
      return &s_bindings[i];
    }
  }
  return NULL;
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

static void touch_handler(const TouchEvent *event, void *context) {
  GPoint p = GPoint(event->x, event->y);
  switch (event->type) {
    case TouchEvent_Touchdown:
      s_active = top_binding();
      s_start = s_last = p;
      s_dragging = false;
      if (s_active && s_active->scroll) {
        s_base = scroll_layer_get_content_offset(s_active->scroll).y;
      }
      break;
    case TouchEvent_PositionUpdate:
      if (!s_active || s_active != top_binding()) {
        s_active = NULL;
        break;
      }
      s_last = p;
      if (!s_dragging && abs(p.y - s_start.y) > DRAG_THRESHOLD) {
        s_dragging = true;
      }
      if (s_dragging && s_active->scroll) {
        set_offset(s_active->scroll, s_base + (p.y - s_start.y));
      }
      break;
    case TouchEvent_Liftoff:
      if (!s_active || s_active != top_binding()) {
        s_active = NULL;
        break;
      }
      if (s_dragging) {
        if (s_active->menu) {
          select_visible_row(s_active);
        }
      } else if (abs(s_last.x - s_start.x) <= DRAG_THRESHOLD && s_active->on_tap) {
        s_active->on_tap(s_start);
      }
      s_active = NULL;
      break;
  }
}

void touch_init(void) {
  touch_service_subscribe(touch_handler, NULL);
}

void touch_deinit(void) {
  touch_service_unsubscribe();
}

static Binding *attach(Window *window, ScrollLayer *scroll, MenuLayer *menu, TouchTapHandler on_tap) {
  for (int i = 0; i < MAX_BINDINGS; i++) {
    if (!s_bindings[i].window) {
      s_bindings[i] = (Binding) { .window = window, .scroll = scroll, .menu = menu, .on_tap = on_tap };
      return &s_bindings[i];
    }
  }
  return NULL;
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
      if (s_active == &s_bindings[i]) {
        s_active = NULL;
      }
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
