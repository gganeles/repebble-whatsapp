// One conversation: message bubbles, newest at the bottom. Select opens the reply menu.
#include <pebble.h>
#include "comms.h"
#include "model.h"
#include "theme.h"
#include "ui.h"

#define MARGIN 4
#define BUBBLE_PAD 4
#define SENDER_HEIGHT 16
#define FOOTER_HEIGHT 14
#define MAX_TEXT_HEIGHT 2000

static Window *s_window;
static TextLayer *s_header;
static MenuLayer *s_menu;
static uint16_t s_chat;
static char s_title[CHAT_NAME_LEN];
static char s_error[64];
static uint16_t s_anchor;  // message to keep selected while an older page loads

static bool is_group(void) {
  Chat *c = model_find_chat(s_chat);
  return c && (c->flags & FLAG_GROUP);
}

static bool has_older_row(void) {
  return g_model.messages_loaded && g_model.messages_more;
}

static int first_message_row(void) {
  return has_older_row() ? 1 : 0;
}

static uint16_t get_num_rows(MenuLayer *menu, uint16_t section, void *ctx) {
  if (!g_model.messages_loaded || g_model.message_count == 0) {
    return 1;
  }
  return g_model.message_count + first_message_row();
}

static Message *message_for_row(int row) {
  int i = row - first_message_row();
  if (!g_model.messages_loaded || i < 0 || i >= g_model.message_count) {
    return NULL;
  }
  return &g_model.messages[i];
}

static int bubble_width(const Layer *layer) {
  GRect b = layer_get_bounds(layer);
  return (b.size.w - 2 * MARGIN) * 85 / 100;
}

static GFont text_font(void) {
  return fonts_get_system_font(FONT_KEY_GOTHIC_18);
}

static bool shows_sender(const Message *m) {
  return is_group() && !(m->flags & FLAG_FROM_ME) && m->sender[0];
}

static int16_t text_height(const Message *m, int width) {
  GSize size = graphics_text_layout_get_content_size(m->text, text_font(), GRect(0, 0, width, MAX_TEXT_HEIGHT),
                                                     GTextOverflowModeWordWrap, GTextAlignmentLeft);
  return size.h + 4;
}

static int16_t get_cell_height(MenuLayer *menu, MenuIndex *index, void *ctx) {
  Message *m = message_for_row(index->row);
  if (!m) {
    return 36;
  }
  int inner = bubble_width(menu_layer_get_layer(menu)) - 2 * BUBBLE_PAD;
  return MARGIN + BUBBLE_PAD + (shows_sender(m) ? SENDER_HEIGHT : 0) + text_height(m, inner) + FOOTER_HEIGHT + BUBBLE_PAD;
}

static void status_text(const Message *m, char *buf, size_t size) {
  char when[12];
  format_time(m->ts, when, sizeof(when));
  const char *more = (m->flags & FLAG_IMAGE) ? "view  " : ((m->flags & FLAG_TRUNCATED) ? "more  " : "");
  if (!(m->flags & FLAG_FROM_ME)) {
    snprintf(buf, size, "%s%s", more, when);
    return;
  }
  if (m->flags & FLAG_FAILED) {
    snprintf(buf, size, "Failed. Select to retry");
    return;
  }
  static const char *labels[] = { "sending", "sent", "delivered", "read" };
  int status = (m->flags & FLAG_STATUS_MASK) >> FLAG_STATUS_SHIFT;
  snprintf(buf, size, "%s%s  %s", more, when, labels[status]);
}

static void draw_row(GContext *gc, const Layer *cell, MenuIndex *index, void *ctx) {
  GRect b = layer_get_bounds(cell);
  Message *m = message_for_row(index->row);
  if (!m) {
    const char *text;
    if (g_model.messages_loaded && g_model.message_count > 0) {
      text = g_model.messages_loading ? "Loading..." : "Load earlier";
    } else if (s_error[0]) {
      text = s_error;
    } else {
      text = g_model.messages_loaded ? "No messages yet" : "Loading...";
    }
    graphics_context_set_text_color(gc, THEME_SUBTLE);
    graphics_draw_text(gc, text, fonts_get_system_font(FONT_KEY_GOTHIC_18_BOLD), GRect(MARGIN, 6, b.size.w - 2 * MARGIN, 24),
                       GTextOverflowModeTrailingEllipsis, GTextAlignmentCenter, NULL);
    return;
  }

  bool mine = m->flags & FLAG_FROM_ME;
  bool highlighted = menu_cell_layer_is_highlighted(cell);
  int bw = bubble_width(cell);
  int inner = bw - 2 * BUBBLE_PAD;
  int x = mine ? b.size.w - MARGIN - bw : MARGIN;
  GRect bubble = GRect(x, MARGIN, bw, b.size.h - MARGIN);

  graphics_context_set_fill_color(gc, mine ? THEME_BUBBLE_ME : THEME_BUBBLE_THEM);
  graphics_fill_rect(gc, bubble, 6, GCornersAll);
  if (highlighted || !PBL_IF_COLOR_ELSE(true, false)) {
    graphics_context_set_stroke_color(gc, highlighted ? THEME_ACCENT : GColorBlack);
    graphics_context_set_stroke_width(gc, highlighted ? 3 : 1);
    graphics_draw_round_rect(gc, bubble, 6);
  }

  int y = MARGIN + BUBBLE_PAD;
  if (shows_sender(m)) {
    graphics_context_set_text_color(gc, THEME_ACCENT);
    graphics_draw_text(gc, m->sender, fonts_get_system_font(FONT_KEY_GOTHIC_14_BOLD),
                       GRect(x + BUBBLE_PAD, y - 2, inner, SENDER_HEIGHT),
                       GTextOverflowModeTrailingEllipsis, GTextAlignmentLeft, NULL);
    y += SENDER_HEIGHT;
  }
  int th = text_height(m, inner);
  graphics_context_set_text_color(gc, (m->flags & FLAG_MEDIA) ? THEME_SUBTLE : GColorBlack);
  graphics_draw_text(gc, m->text, text_font(), GRect(x + BUBBLE_PAD, y - 4, inner, th),
                     GTextOverflowModeWordWrap, GTextAlignmentLeft, NULL);
  y += th;

  char footer[32];
  status_text(m, footer, sizeof(footer));
  graphics_context_set_text_color(gc, (m->flags & FLAG_FAILED) ? THEME_ERROR : THEME_SUBTLE);
  graphics_draw_text(gc, footer, fonts_get_system_font(FONT_KEY_GOTHIC_14), GRect(x + BUBBLE_PAD, y - 4, inner, FOOTER_HEIGHT + 2),
                     GTextOverflowModeTrailingEllipsis, GTextAlignmentRight, NULL);
}

static void load_older(void) {
  if (!has_older_row() || g_model.messages_loading || g_model.message_count == 0) {
    return;
  }
  s_anchor = g_model.messages[0].handle;
  comms_get_messages(s_chat, g_model.messages[0].handle);
  menu_layer_reload_data(s_menu);
}

static void selection_changed(MenuLayer *menu, MenuIndex new_index, MenuIndex old_index, void *ctx) {
  if (new_index.row == 0 && has_older_row()) {
    load_older();
  }
}

static void select_click(MenuLayer *menu, MenuIndex *index, void *ctx) {
  Message *m = message_for_row(index->row);
  if (!g_model.messages_loaded && s_error[0]) {
    s_error[0] = '\0';
    comms_get_messages(s_chat, 0);
    menu_layer_reload_data(s_menu);
    return;
  }
  if (!m && g_model.messages_loaded && g_model.message_count > 0) {
    load_older();
    return;
  }
  if (m && (m->flags & FLAG_FAILED)) {
    char text[MSG_TEXT_LEN];
    model_copy(text, m->text, sizeof(text));
    int i = m - g_model.messages;
    memmove(m, m + 1, sizeof(Message) * (g_model.message_count - i - 1));
    g_model.message_count--;
    comms_send_text(s_chat, text);
    return;
  }
  if (m && m->handle) {
    message_window_push(s_chat, m);
    return;
  }
  reply_menu_open(s_chat);
}

static void select_long_click(MenuLayer *menu, MenuIndex *index, void *ctx) {
  reply_menu_open(s_chat);
}

static void window_load(Window *window) {
  Layer *root = window_get_root_layer(window);
  GRect b = layer_get_bounds(root);
  window_set_background_color(window, THEME_CHAT_BG);

  Chat *c = model_find_chat(s_chat);
  model_copy(s_title, c ? c->name : "Chat", sizeof(s_title));
  s_header = text_layer_create(GRect(0, 0, b.size.w, HEADER_HEIGHT));
  text_layer_set_background_color(s_header, THEME_ACCENT);
  text_layer_set_text_color(s_header, THEME_ON_ACCENT);
  text_layer_set_font(s_header, fonts_get_system_font(FONT_KEY_GOTHIC_18_BOLD));
  text_layer_set_text_alignment(s_header, GTextAlignmentCenter);
  text_layer_set_overflow_mode(s_header, GTextOverflowModeTrailingEllipsis);
  text_layer_set_text(s_header, s_title);
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
  // We draw our own selection outline, so the menu's highlight blends in.
  menu_layer_set_normal_colors(s_menu, THEME_CHAT_BG, GColorBlack);
  menu_layer_set_highlight_colors(s_menu, THEME_CHAT_BG, GColorBlack);
  menu_layer_set_click_config_onto_window(s_menu, window);
  layer_add_child(root, menu_layer_get_layer(s_menu));

  s_error[0] = '\0';
  s_anchor = 0;
  comms_get_messages(s_chat, 0);
  if (!g_model.replies_loaded) {
    comms_get_replies();
  }
}

static void window_unload(Window *window) {
  comms_close_chat(s_chat);
  menu_layer_destroy(s_menu);
  text_layer_destroy(s_header);
  window_destroy(s_window);
  s_window = NULL;
  s_menu = NULL;
}

void conversation_window_push(uint16_t chat) {
  if (s_window) {
    return;
  }
  s_chat = chat;
  s_window = window_create();
  window_set_window_handlers(s_window, (WindowHandlers) {
    .load = window_load,
    .unload = window_unload,
  });
  window_stack_push(s_window, true);
}

void conversation_window_refresh(bool scroll_to_newest) {
  if (!s_menu) {
    return;
  }
  menu_layer_reload_data(s_menu);
  uint16_t rows = get_num_rows(s_menu, 0, NULL);
  if (scroll_to_newest && rows > 0) {
    menu_layer_set_selected_index(s_menu, MenuIndex(0, rows - 1), MenuRowAlignBottom, false);
  } else if (s_anchor && !g_model.messages_loading) {
    Message *anchor = model_find_message(s_anchor);
    if (anchor) {
      int row = (anchor - g_model.messages) + first_message_row();
      menu_layer_set_selected_index(s_menu, MenuIndex(0, row), MenuRowAlignTop, false);
    }
    s_anchor = 0;
  }
}

void conversation_window_on_error(uint8_t err, const char *text) {
  snprintf(s_error, sizeof(s_error), "%s", text[0] ? text : "Couldn't load messages");
  if (s_menu) {
    menu_layer_reload_data(s_menu);
  }
}
