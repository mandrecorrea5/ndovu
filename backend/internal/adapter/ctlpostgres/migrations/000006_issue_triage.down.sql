DROP TABLE IF EXISTS issue_comments;
ALTER TABLE issue_states DROP COLUMN IF EXISTS assignee_user_id;
ALTER TABLE issue_states DROP CONSTRAINT IF EXISTS issue_states_status_check;
ALTER TABLE issue_states ADD CONSTRAINT issue_states_status_check
    CHECK (status IN ('open', 'resolved', 'ignored'));
