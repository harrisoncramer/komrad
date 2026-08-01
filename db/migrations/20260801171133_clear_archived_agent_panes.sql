UPDATE "agents" SET "pane_id" = '' WHERE "archived_at" IS NOT NULL AND "pane_id" <> '';
