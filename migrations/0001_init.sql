-- MajSpirit 初始表结构
--
-- 对应任务书 §2（用户系统）与 §6（历史牌局）。
--
-- 关于牌的存储：牌组统一用 internal/mahjong 的紧凑记法存成 TEXT，
-- 例如 "123m456s789p11z"。这样数据库里的内容用肉眼就能核对，
-- 与代码、测试用例、日志共用同一套表示，排查问题时不需要额外的解码工具。

BEGIN;

-- ── 用户（任务书 §2）──────────────────────────────────────────────

CREATE TABLE users (
    id            BIGSERIAL   PRIMARY KEY,
    username      TEXT        NOT NULL,
    -- 只存 bcrypt 哈希，明文密码绝不落库
    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT users_username_not_blank CHECK (btrim(username) <> '')
);

-- 用户名唯一，且登录查询走的正是这个索引
CREATE UNIQUE INDEX users_username_key ON users (lower(username));

-- ── 牌局（任务书 §6）──────────────────────────────────────────────

-- 一场游戏：四小局
CREATE TABLE games (
    id          BIGSERIAL   PRIMARY KEY,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,

    CONSTRAINT games_finished_after_started
        CHECK (finished_at IS NULL OR finished_at >= started_at)
);

-- 一场游戏里的四个座位
CREATE TABLE game_seats (
    game_id     BIGINT NOT NULL REFERENCES games (id) ON DELETE CASCADE,
    seat        SMALLINT NOT NULL,          -- 0-3；简化规则中第 N 小局的庄家为 seat N-1
    user_id     BIGINT NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    final_score INTEGER NOT NULL DEFAULT 0,
    placement   SMALLINT,                   -- 顺位 1-4，游戏结束后写入

    PRIMARY KEY (game_id, seat),
    CONSTRAINT game_seats_seat_range    CHECK (seat BETWEEN 0 AND 3),
    CONSTRAINT game_seats_placement_range
        CHECK (placement IS NULL OR placement BETWEEN 1 AND 4)
);

CREATE INDEX game_seats_user_idx ON game_seats (user_id, game_id);
-- 查询某位玩家过往牌局的顺位（任务书 §6 第一条）
CREATE INDEX game_seats_user_placement_idx ON game_seats (user_id, placement);

-- 每个小局一行的汇总
CREATE TABLE game_rounds (
    game_id       BIGINT   NOT NULL REFERENCES games (id) ON DELETE CASCADE,
    round_index   SMALLINT NOT NULL,        -- 1-4
    dealer_seat   SMALLINT NOT NULL,        -- 本小局庄家
    ended_by      TEXT,                     -- 'win' 或 'draw'（牌山摸空）
    winner_seat   SMALLINT,                 -- 和牌者；流局时为 NULL
    is_tsumo      BOOLEAN,                  -- true=自摸 false=荣和；流局时为 NULL
    winning_tile  TEXT,                     -- 和牌的那张牌，如 "5p"

    PRIMARY KEY (game_id, round_index),
    CONSTRAINT game_rounds_index_range CHECK (round_index BETWEEN 1 AND 4),
    CONSTRAINT game_rounds_ended_by
        CHECK (ended_by IS NULL OR ended_by IN ('win', 'draw'))
);

-- 每个小局的初始手牌（任务书 §6 要求可查"初始手牌"）
CREATE TABLE game_initial_hands (
    game_id      BIGINT   NOT NULL,
    round_index  SMALLINT NOT NULL,
    seat         SMALLINT NOT NULL,
    tiles        TEXT     NOT NULL,         -- 紧凑记法，庄家 14 张、闲家 13 张

    PRIMARY KEY (game_id, round_index, seat),
    FOREIGN KEY (game_id, round_index)
        REFERENCES game_rounds (game_id, round_index) ON DELETE CASCADE
);

-- 每个小局的牌山（任务书 §6 要求可查"牌山"）
-- 存完整的摸牌顺序，便于复核整局是否被"操控"
CREATE TABLE game_walls (
    game_id     BIGINT   NOT NULL,
    round_index SMALLINT NOT NULL,
    tiles       TEXT     NOT NULL,          -- 136 张的完整洗牌结果

    PRIMARY KEY (game_id, round_index),
    FOREIGN KEY (game_id, round_index)
        REFERENCES game_rounds (game_id, round_index) ON DELETE CASCADE
);

-- 每一回合的动作（任务书 §6 要求可查"每一回合的摸牌 & 出牌"）
CREATE TABLE game_events (
    game_id     BIGINT   NOT NULL,
    round_index SMALLINT NOT NULL,
    seq         INTEGER  NOT NULL,          -- 小局内的动作序号，从 1 开始
    seat        SMALLINT NOT NULL,
    action      TEXT     NOT NULL,          -- 'draw' | 'discard' | 'tsumo' | 'ron'
    tile        TEXT     NOT NULL,

    PRIMARY KEY (game_id, round_index, seq),
    CONSTRAINT game_events_action
        CHECK (action IN ('draw', 'discard', 'tsumo', 'ron')),
    FOREIGN KEY (game_id, round_index)
        REFERENCES game_rounds (game_id, round_index) ON DELETE CASCADE
);

-- 回放某一小局时按 seq 顺序取全部动作
CREATE INDEX game_events_replay_idx ON game_events (game_id, round_index, seq);

COMMIT;
