// Shown while the phone server is unreachable, unconfigured, or not linked to WhatsApp yet.
#include <pebble.h>
#include "comms.h"
#include "theme.h"
#include "ui.h"

static Window *s_window;
static TextLayer *s_title;
static TextLayer *s_body;
static TextLayer *s_hint;
static AppTimer *s_poll_timer;

static ServerState s_state = STATE_UNKNOWN;
static char s_body_buf[100];

static void poll_cb(void *data) {
  s_poll_timer = NULL;
  comms_get_status();
}

static void schedule_poll(uint32_t ms) {
  if (s_poll_timer) {
    app_timer_cancel(s_poll_timer);
  }
  s_poll_timer = app_timer_register(ms, poll_cb, NULL);
}

static void render(const char *text) {
  if (!s_window) {
    return;
  }
  const char *title = "WhatsApp";
  const char *hint = "";
  GFont body_font = fonts_get_system_font(FONT_KEY_GOTHIC_18);
  switch (s_state) {
    case STATE_UNKNOWN:
      title = "Starting";
      snprintf(s_body_buf, sizeof(s_body_buf), "Waiting for the phone...");
      break;
    case STATE_UNREACHABLE:
      title = "Server offline";
      snprintf(s_body_buf, sizeof(s_body_buf), "Start pebblewa in Termux on your phone.");
      hint = "Select: retry";
      break;
    case STATE_NOT_CONFIGURED:
      title = "Setup needed";
      snprintf(s_body_buf, sizeof(s_body_buf), "%s", text[0] ? text : "Open this app's settings in the Pebble app.");
      hint = "Select: retry";
      break;
    case STATE_UNPAIRED:
    case STATE_LOGGED_OUT:
      title = "Link WhatsApp";
      snprintf(s_body_buf, sizeof(s_body_buf), "%s", text[0] ? text : "Get a code to link this watch.");
      hint = "Select: get code";
      break;
    case STATE_PAIRING:
      title = "Enter code";
      snprintf(s_body_buf, sizeof(s_body_buf), "%s", text);
      body_font = fonts_get_system_font(PBL_DISPLAY_WIDTH >= 200 ? FONT_KEY_BITHAM_30_BLACK : FONT_KEY_GOTHIC_28_BOLD);
      hint = "WhatsApp > Linked devices > Link with phone number";
      break;
    case STATE_CONNECTING:
      title = "Connecting";
      snprintf(s_body_buf, sizeof(s_body_buf), "Connecting to WhatsApp...");
      break;
    case STATE_CONNECTED:
      title = "Connected";
      s_body_buf[0] = '\0';
      break;
  }
  text_layer_set_text(s_title, title);
  text_layer_set_font(s_body, body_font);
  text_layer_set_text(s_body, s_body_buf);
  text_layer_set_text(s_hint, hint);
}

static void select_click(ClickRecognizerRef recognizer, void *context) {
  if (s_state == STATE_UNPAIRED || s_state == STATE_LOGGED_OUT) {
    text_layer_set_text(s_body, "Requesting code...");
    comms_pair();
  } else {
    comms_get_status();
  }
}

static void click_config(void *context) {
  window_single_click_subscribe(BUTTON_ID_SELECT, select_click);
}

static void window_load(Window *window) {
  Layer *root = window_get_root_layer(window);
  GRect b = layer_get_bounds(root);
  window_set_background_color(window, THEME_ACCENT);
  int pad = PBL_IF_ROUND_ELSE(24, 8);

  s_title = text_layer_create(GRect(pad, 16, b.size.w - 2 * pad, 32));
  text_layer_set_font(s_title, fonts_get_system_font(FONT_KEY_GOTHIC_28_BOLD));

  s_body = text_layer_create(GRect(pad, 56, b.size.w - 2 * pad, 90));

  s_hint = text_layer_create(GRect(pad, b.size.h - 60, b.size.w - 2 * pad, 56));
  text_layer_set_font(s_hint, fonts_get_system_font(FONT_KEY_GOTHIC_14));

  TextLayer *layers[] = { s_title, s_body, s_hint };
  for (unsigned i = 0; i < ARRAY_LENGTH(layers); i++) {
    text_layer_set_background_color(layers[i], GColorClear);
    text_layer_set_text_color(layers[i], THEME_ON_ACCENT);
    text_layer_set_text_alignment(layers[i], PBL_IF_ROUND_ELSE(GTextAlignmentCenter, GTextAlignmentLeft));
    layer_add_child(root, text_layer_get_layer(layers[i]));
  }
  render("");
}

static void window_unload(Window *window) {
  text_layer_destroy(s_title);
  text_layer_destroy(s_body);
  text_layer_destroy(s_hint);
  if (s_poll_timer) {
    app_timer_cancel(s_poll_timer);
    s_poll_timer = NULL;
  }
  window_destroy(s_window);
  s_window = NULL;
}

void status_window_push(void) {
  if (s_window) {
    return;
  }
  s_window = window_create();
  window_set_click_config_provider(s_window, click_config);
  window_set_window_handlers(s_window, (WindowHandlers) {
    .load = window_load,
    .unload = window_unload,
  });
  window_stack_push(s_window, true);
}

void status_window_on_status(ServerState state, uint8_t err, const char *text) {
  s_state = state;
  if (state == STATE_CONNECTED) {
    if (!chat_list_window_is_loaded()) {
      chat_list_window_push();
    }
    if (s_window) {
      window_stack_remove(s_window, true);
    }
    return;
  }
  status_window_push();
  render(text);
  // Keep checking while waiting for something to change on the phone.
  if (state == STATE_PAIRING || state == STATE_CONNECTING || state == STATE_UNKNOWN) {
    schedule_poll(4000);
  } else if (state == STATE_UNREACHABLE) {
    schedule_poll(10000);
  }
}
