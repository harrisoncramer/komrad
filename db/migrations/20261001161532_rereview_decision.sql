-- Set comment to column: "review_decision" on table: "pull_requests"
COMMENT ON COLUMN "pull_requests"."review_decision" IS 'Where the forge says the reviews stand, in komrad''s own words: pending, approved, changes_requested or rereview_requested, null while it has said nothing.';
