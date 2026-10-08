// Reply options for a conversation: dictation first, then the canned replies.
#include <pebble.h>
#include "comms.h"
#include "model.h"
#include "theme.h"
#include "ui.h"

#define DICTATE_ACTION ((void *)-1)

static ActionMenuLevel *s_root;
static uint16_t s_chat;
static bool s_waiting_for_replies;
static bool s_start_dictation;

#if defined(PBL_MICROPHONE)
static DictationSession *s_dictation;

static void dictation_cb(DictationSession *session, DictationSessionStatus status, char *transcription, void *ctx) {
  if (status == DictationSessionStatusSuccess && transcription && transcription[0]) {
    comms_send_text(s_chat, transcription);
  } else if (status != DictationSessionStatusFailureTranscriptionRejected &&
             status != DictationSessionStatusFailureTranscriptionRejectedWithError) {
    APP_LOG(APP_LOG_LEVEL_WARNING, "Dictation failed: %d", (int)status);
    vibes_short_pulse();
  }
}

static void start_dictation(void) {
  if (!s_dictation) {
    s_dictation = dictation_session_create(512, dictation_cb, NULL);
  }
  if (s_dictation) {
    dictation_session_start(s_dictation);
  }
}
#endif

static void action_performed(ActionMenu *menu, const ActionMenuItem *item, void *ctx) {
  void *data = action_menu_item_get_action_data(item);
  if (data == DICTATE_ACTION) {
    // Start dictation after the menu has closed.
    s_start_dictation = true;
    return;
  }
  int i = (int)(intptr_t)data;
  if (i >= 0 && i < g_model.reply_count) {
    comms_send_text(s_chat, g_model.replies[i]);
  }
}

static void did_close(ActionMenu *menu, const ActionMenuItem *performed, void *ctx) {
  action_menu_hierarchy_destroy(s_root, NULL, NULL);
  s_root = NULL;
#if defined(PBL_MICROPHONE)
  if (s_start_dictation) {
    s_start_dictation = false;
    start_dictation();
  }
#endif
}

static void open_menu(void) {
  int count = g_model.reply_count + PBL_IF_MICROPHONE_ELSE(1, 0);
  if (count == 0) {
    vibes_short_pulse();
    return;
  }
  s_root = action_menu_level_create(count);
#if defined(PBL_MICROPHONE)
  action_menu_level_add_action(s_root, "Dictate", action_performed, DICTATE_ACTION);
#endif
  for (int i = 0; i < g_model.reply_count; i++) {
    action_menu_level_add_action(s_root, g_model.replies[i], action_performed, (void *)(intptr_t)i);
  }
  ActionMenuConfig config = {
    .root_level = s_root,
    .colors = {
      .background = THEME_ACCENT,
      .foreground = THEME_ON_ACCENT,
    },
    .align = ActionMenuAlignTop,
    .did_close = did_close,
  };
  action_menu_open(&config);
}

void reply_menu_open(uint16_t chat) {
  s_chat = chat;
  if (s_root) {
    return;
  }
  if (!g_model.replies_loaded) {
    s_waiting_for_replies = true;
    comms_get_replies();
#if defined(PBL_MICROPHONE)
    // Don't make the user wait: dictation works without the list.
    s_waiting_for_replies = false;
    open_menu();
#endif
    return;
  }
  open_menu();
}

void reply_menu_on_replies_loaded(void) {
  if (s_waiting_for_replies) {
    s_waiting_for_replies = false;
    open_menu();
  }
}
