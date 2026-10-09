// Recent chats, newest first, paged in as you scroll.
#include <pebble.h>
#include "comms.h"
#include "model.h"
#include "theme.h"
#include "touch.h"
#include "ui.h"

#define ROW_HEIGHT 50
#define PAD 4

static Window *s_window;
static TextLayer *s_header;
static MenuLayer *s_menu;
static char s_error[64];

static bool has_more_row(void) {
  return g_model.chat_count > 0 && (g_model.chats_more || g_model.chats_loading);
}

static uint16_t get_num_rows(MenuLayer *menu, uint16_t section, void *ctx) {
  if (g_model.chat_count == 0) {
    return 1;  // placeholder row
  }
  return g_model.chat_count + (has_more_row() ? 1 : 0);
}

static int16_t get_cell_height(MenuLayer *menu, MenuIndex *index, void *ctx) {
  return ROW_HEIGHT;
}

static void draw_placeholder(GContext *gc, const Layer *cell, const char *text) {
  GRect b = layer_get_bounds(cell);
  graphics_draw_text(gc, text, fonts_get_system_font(FONT_KEY_GOTHIC_18),
                     GRect(PAD, b.size.h / 2 - 12, b.size.w - 2 * PAD, 24),
                     GTextOverflowModeTrailingEllipsis, GTextAlignmentCenter, NULL);
}

static void draw_row(GContext *gc, const Layer *cell, MenuIndex *index, void *ctx) {
  if (g_model.chat_count == 0) {
    const char *text = s_error[0] ? s_error : (g_model.chats_loading ? "Loading chats..." : "No chats yet");
    draw_placeholder(gc, cell, text);
    return;
  }
  if (index->row >= g_model.chat_count) {
    draw_placeholder(gc, cell, "Loading more...");
    return;
  }
  Chat *c = &g_model.chats[index->row];
  GRect b = layer_get_bounds(cell);
  bool highlighted = menu_cell_layer_is_highlighted(cell);
  GColor fg = highlighted ? THEME_ON_ACCENT : GColorBlack;
  graphics_context_set_text_color(gc, fg);
  int inset = PBL_IF_ROUND_ELSE(16, PAD);
  int w = b.size.w - 2 * inset;

  char when[12];
  format_time(c->ts, when, sizeof(when));
  GFont small = fonts_get_system_font(FONT_KEY_GOTHIC_14);
  GSize when_size = graphics_text_layout_get_content_size(when, small, GRect(0, 0, w, 20),
                                                          GTextOverflowModeFill, GTextAlignmentRight);
  graphics_draw_text(gc, when, small, GRect(inset, 4, w, 18),
                     GTextOverflowModeFill, GTextAlignmentRight, NULL);
  graphics_draw_text(gc, c->name, fonts_get_system_font(FONT_KEY_GOTHIC_18_BOLD),
                     GRect(inset, -2, w - when_size.w - 4, 22),
                     GTextOverflowModeTrailingEllipsis, GTextAlignmentLeft, NULL);

  int badge_w = 0;
  if (c->unread) {
    char count[4];
    snprintf(count, sizeof(count), "%d", c->unread);
    badge_w = c->unread > 9 ? 24 : 18;
    GRect badge = GRect(inset + w - badge_w, 26, badge_w, 18);
    graphics_context_set_fill_color(gc, highlighted ? THEME_ON_ACCENT : THEME_BADGE);
    graphics_fill_rect(gc, badge, 9, GCornersAll);
    graphics_context_set_text_color(gc, highlighted ? THEME_ACCENT : THEME_ON_ACCENT);
    graphics_draw_text(gc, count, fonts_get_system_font(FONT_KEY_GOTHIC_14_BOLD), GRect(badge.origin.x, 25, badge_w, 18),
                       GTextOverflowModeFill, GTextAlignmentCenter, NULL);
    graphics_context_set_text_color(gc, fg);
  }
  graphics_context_set_text_color(gc, highlighted ? THEME_ON_ACCENT : THEME_SUBTLE);
  graphics_draw_text(gc, c->preview, fonts_get_system_font(FONT_KEY_GOTHIC_18),
                     GRect(inset, 22, w - badge_w - (badge_w ? 4 : 0), 22),
                     GTextOverflowModeTrailingEllipsis, GTextAlignmentLeft, NULL);
}

static void maybe_load_more(uint16_t row) {
  if (g_model.chats_more && !g_model.chats_loading && row + 2 >= g_model.chat_count) {
    comms_get_chats(g_model.chat_pages);
    menu_layer_reload_data(s_menu);
  }
}

static void selection_changed(MenuLayer *menu, MenuIndex new_index, MenuIndex old_index, void *ctx) {
  maybe_load_more(new_index.row);
}

static void select_click(MenuLayer *menu, MenuIndex *index, void *ctx) {
  if (g_model.chat_count == 0) {
    s_error[0] = '\0';
    comms_get_chats(0);
    menu_layer_reload_data(s_menu);
    return;
  }
  if (index->row < g_model.chat_count) {
    conversation_window_push(g_model.chats[index->row].handle);
  }
}

static void select_long_click(MenuLayer *menu, MenuIndex *index, void *ctx) {
  // Refresh from the top.
  s_error[0] = '\0';
  comms_get_chats(0);
  menu_layer_set_selected_index(s_menu, MenuIndex(0, 0), MenuRowAlignTop, false);
  menu_layer_reload_data(s_menu);
}

static void tap(GPoint point) {
  int row = touch_menu_row_at(s_menu, point, get_num_rows, get_cell_height);
  if (row < 0) {
    return;
  }
  MenuIndex index = MenuIndex(0, row);
  menu_layer_set_selected_index(s_menu, index, MenuRowAlignNone, false);
  select_click(s_menu, &index, NULL);
}

static void window_load(Window *window) {
  Layer *root = window_get_root_layer(window);
  GRect b = layer_get_bounds(root);

  s_header = text_layer_create(GRect(0, 0, b.size.w, HEADER_HEIGHT));
  text_layer_set_background_color(s_header, THEME_ACCENT);
  text_layer_set_text_color(s_header, THEME_ON_ACCENT);
  text_layer_set_font(s_header, fonts_get_system_font(FONT_KEY_GOTHIC_18_BOLD));
  text_layer_set_text_alignment(s_header, GTextAlignmentCenter);
  text_layer_set_text(s_header, "WhatsApp");
  layer_add_child(root, text_layer_get_layer(s_header));

  s_menu = menu_layer_create(GRect(0, HEADER_HEIGHT, b.size.w, b.size.h - HEADER_HEIGHT));
  menu_layer_set_callbacks(s_menu, NULL, (MenuLayerCallbacks) {
    .get_num_rows = get_num_rows,
    .get_cell_height = get_cell_height,
    .draw_row = draw_row,
    .select_click = select_click,
    .select_long_click = select_long_click,
    .selection_changed = selection_changed,
  });
  menu_layer_set_normal_colors(s_menu, THEME_BG, GColorBlack);
  menu_layer_set_highlight_colors(s_menu, THEME_ACCENT, THEME_ON_ACCENT);
  menu_layer_set_click_config_onto_window(s_menu, window);
  layer_add_child(root, menu_layer_get_layer(s_menu));
  touch_attach_menu(window, s_menu, get_num_rows, get_cell_height, tap);

  comms_get_chats(0);
}

static void window_unload(Window *window) {
  touch_detach(window);
  menu_layer_destroy(s_menu);
  text_layer_destroy(s_header);
  window_destroy(s_window);
  s_window = NULL;
  s_menu = NULL;
}

void chat_list_window_push(void) {
  if (s_window) {
    return;
  }
  s_window = window_create();
  window_set_window_handlers(s_window, (WindowHandlers) {
    .load = window_load,
    .unload = window_unload,
  });
  window_stack_push(s_window, true);
}

bool chat_list_window_is_loaded(void) {
  return s_window != NULL;
}

void chat_list_window_refresh(void) {
  if (s_menu) {
    if (g_model.chat_count > 0) {
      s_error[0] = '\0';
    }
    menu_layer_reload_data(s_menu);
  }
}

void chat_list_window_on_error(uint8_t err, const char *text) {
  snprintf(s_error, sizeof(s_error), "%s", text[0] ? text : "Couldn't load chats");
  chat_list_window_refresh();
}
