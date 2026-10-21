CREATE TABLE servers (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  ip VARCHAR(50) NOT NULL,
  rcon VARCHAR(50) NOT NULL,
  hostname VARCHAR(100) NOT NULL,
  tv_ip VARCHAR(100),
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE teams (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(100),
  shorthandle VARCHAR(50),
  flag VARCHAR(2),
  link VARCHAR(255),
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE seasons (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(100),
  active TINYINT(1),
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE sf_guard_user (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(128) NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE matchs (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  ip VARCHAR(50),
  server_id BIGINT,
  season_id BIGINT,
  team_a BIGINT,
  team_a_flag VARCHAR(2),
  team_a_name VARCHAR(25),
  team_b BIGINT,
  team_b_flag VARCHAR(2),
  team_b_name VARCHAR(25),
  status INT,
  is_paused TINYINT(1),
  score_a INT,
  score_b INT,
  max_round INT NOT NULL,
  rules VARCHAR(200) NOT NULL,
  overtime_startmoney INT,
  overtime_max_round INT,
  config_full_score TINYINT(1),
  config_ot TINYINT(1),
  config_streamer TINYINT(1),
  config_knife_round TINYINT(1),
  config_switch_auto TINYINT(1),
  config_auto_change_password TINYINT(1),
  config_password VARCHAR(50),
  config_heatmap TINYINT(1),
  config_authkey VARCHAR(200),
  enable TINYINT(1),
  map_selection_mode VARCHAR(20),
  ingame_enable TINYINT(1),
  current_map BIGINT,
  force_zoom_match TINYINT(1),
  identifier_id VARCHAR(100),
  startdate DATETIME,
  auto_start TINYINT(1),
  auto_start_time INT,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY (server_id) REFERENCES servers (id) ON DELETE SET NULL,
  FOREIGN KEY (team_a) REFERENCES teams (id) ON DELETE SET NULL,
  FOREIGN KEY (team_b) REFERENCES teams (id) ON DELETE SET NULL,
  FOREIGN KEY (season_id) REFERENCES seasons (id) ON DELETE SET NULL
);

CREATE TABLE maps (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  match_id BIGINT NOT NULL,
  map_name VARCHAR(50),
  score_1 INT,
  score_2 INT,
  current_side VARCHAR(2),
  status INT,
  maps_for VARCHAR(10),
  nb_ot BIGINT,
  identifier_id BIGINT,
  tv_record_file VARCHAR(255),
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY (match_id) REFERENCES matchs (id) ON DELETE CASCADE
);

ALTER TABLE matchs ADD FOREIGN KEY (current_map) REFERENCES maps (id) ON DELETE CASCADE;

CREATE TABLE maps_score (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  map_id BIGINT NOT NULL,
  type_score VARCHAR(10),
  score1_side1 INT,
  score1_side2 INT,
  score2_side1 INT,
  score2_side2 INT,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY (map_id) REFERENCES maps (id) ON DELETE CASCADE
);

CREATE TABLE players (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  match_id BIGINT NOT NULL,
  map_id BIGINT NOT NULL,
  player_key VARCHAR(255),
  team VARCHAR(10) DEFAULT 'other',
  ip VARCHAR(255),
  steamid VARCHAR(255),
  first_side VARCHAR(10),
  current_side VARCHAR(10),
  pseudo VARCHAR(255),
  nb_kill INT DEFAULT 0,
  assist INT DEFAULT 0,
  death INT DEFAULT 0,
  point INT DEFAULT 0,
  hs INT DEFAULT 0,
  defuse INT DEFAULT 0,
  bombe INT DEFAULT 0,
  tk INT DEFAULT 0,
  nb1 INT DEFAULT 0,
  nb2 INT DEFAULT 0,
  nb3 INT DEFAULT 0,
  nb4 INT DEFAULT 0,
  nb5 INT DEFAULT 0,
  nb1kill INT DEFAULT 0,
  nb2kill INT DEFAULT 0,
  nb3kill INT DEFAULT 0,
  nb4kill INT DEFAULT 0,
  nb5kill INT DEFAULT 0,
  pluskill INT DEFAULT 0,
  firstkill INT DEFAULT 0,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY (match_id) REFERENCES matchs (id) ON DELETE CASCADE,
  FOREIGN KEY (map_id) REFERENCES maps (id) ON DELETE CASCADE
);

CREATE TABLE players_snapshot (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  player_id BIGINT NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY (player_id) REFERENCES players (id) ON DELETE CASCADE
);

CREATE TABLE round_summary (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  match_id BIGINT NOT NULL,
  map_id BIGINT NOT NULL,
  bomb_planted TINYINT(1),
  bomb_defused TINYINT(1),
  bomb_exploded TINYINT(1),
  win_type VARCHAR(20) DEFAULT 'normal',
  team_win VARCHAR(1),
  ct_win TINYINT(1),
  t_win TINYINT(1),
  score_a INT,
  score_b INT,
  best_killer BIGINT,
  best_killer_nb INT,
  best_killer_fk TINYINT(1),
  best_action_type TEXT,
  best_action_param TEXT,
  backup_file_name VARCHAR(255),
  round_id INT,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY (match_id) REFERENCES matchs (id) ON DELETE CASCADE,
  FOREIGN KEY (map_id) REFERENCES maps (id) ON DELETE CASCADE,
  FOREIGN KEY (best_killer) REFERENCES players (id) ON DELETE SET NULL
);

CREATE TABLE player_kill (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  match_id BIGINT NOT NULL,
  map_id BIGINT NOT NULL,
  killer_id BIGINT,
  killed_id BIGINT,
  round_id INT,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY (match_id) REFERENCES matchs (id) ON DELETE CASCADE,
  FOREIGN KEY (map_id) REFERENCES maps (id) ON DELETE CASCADE,
  FOREIGN KEY (killer_id) REFERENCES players (id) ON DELETE CASCADE,
  FOREIGN KEY (killed_id) REFERENCES players (id) ON DELETE CASCADE
);

CREATE TABLE `round` (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  match_id BIGINT NOT NULL,
  map_id BIGINT NOT NULL,
  kill_id BIGINT,
  round_id BIGINT,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY (match_id) REFERENCES matchs (id) ON DELETE CASCADE,
  FOREIGN KEY (map_id) REFERENCES maps (id) ON DELETE CASCADE,
  FOREIGN KEY (kill_id) REFERENCES player_kill (id) ON DELETE SET NULL
);

CREATE TABLE players_heatmap (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  match_id BIGINT NOT NULL,
  map_id BIGINT NOT NULL,
  player_id BIGINT,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY (match_id) REFERENCES matchs (id) ON DELETE CASCADE,
  FOREIGN KEY (map_id) REFERENCES maps (id) ON DELETE CASCADE,
  FOREIGN KEY (player_id) REFERENCES players (id) ON DELETE CASCADE
);
