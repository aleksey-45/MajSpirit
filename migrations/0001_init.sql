-- MajSpirit 初始表结构
--
-- 对应任务书 §2（用户系统）与 §6（历史牌局）。
--
-- 本项目采用**完整的立直麻将规则**，因此除任务书明确要求的内容外，
-- 还需要记录鸣牌、立直、宝牌指示牌、役种、翻数与符数，
-- 否则历史牌局无法复核「这一手到底该算几分」。
-- 算分规则见 docs/立直麻将算分规则.md。
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

-- 一场游戏（东风战 / 半庄战）
CREATE TABLE games (
    id           BIGSERIAL   PRIMARY KEY,
    -- 采用的规则变体快照。规则差异必须随牌局一起存档，
    -- 否则以后改了默认规则，旧牌局就复核不出来了。
    rules        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at  TIMESTAMPTZ,

    CONSTRAINT games_finished_after_started
        CHECK (finished_at IS NULL OR finished_at >= started_at)
);

-- 一场游戏里的四个座位
CREATE TABLE game_seats (
    game_id     BIGINT NOT NULL REFERENCES games (id) ON DELETE CASCADE,
    seat        SMALLINT NOT NULL,          -- 0-3
    user_id     BIGINT NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    initial_score INTEGER NOT NULL,         -- 起手点数，标准规则为 25000
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

-- ── 小局 ──────────────────────────────────────────────────────────

-- 每个小局一行的汇总
CREATE TABLE game_rounds (
    game_id       BIGINT   NOT NULL REFERENCES games (id) ON DELETE CASCADE,
    round_index   SMALLINT NOT NULL,        -- 小局序号，从 1 开始
    round_wind    TEXT     NOT NULL,        -- 场风，紧凑记法单张，如 "1z"（东）
    hand_number   SMALLINT NOT NULL,        -- 第几本场（本场棒数量）
    dealer_seat   SMALLINT NOT NULL,        -- 本小局庄家
    -- 结束方式：'win' 和牌 / 'draw' 荒牌流局 / 'abortive' 途中流局
    ended_by      TEXT     NOT NULL,
    abortive_reason TEXT,                   -- 途中流局原因，见下方 CHECK
    winner_seat   SMALLINT,                 -- 和牌者；流局时为 NULL
    is_tsumo      BOOLEAN,                  -- true=自摸 false=荣和；流局时为 NULL
    winning_tile  TEXT,                     -- 和牌的那张牌，如 "5p"
    -- 供托：本小局结束时场上累积的立直棒数量
    riichi_sticks SMALLINT NOT NULL DEFAULT 0,

    PRIMARY KEY (game_id, round_index),
    CONSTRAINT game_rounds_ended_by
        CHECK (ended_by IN ('win', 'draw', 'abortive')),
    CONSTRAINT game_rounds_abortive_reason
        CHECK (abortive_reason IS NULL OR abortive_reason IN
               ('kyuushu_kyuuhai',   -- 九种九牌
                'suufon_renda',      -- 四风连打
                'suucha_riichi',     -- 四家立直
                'suukantsu',         -- 四杠散了
                'sancha_hou'))       -- 三家和了
);

-- 每个小局的初始手牌（任务书 §6 要求可查「初始手牌」）
--
-- 注意这里存的是**发牌那一刻**的手牌：四家各 13 张。
-- 庄家的第 14 张是它的第一次摸牌，会作为一条 'draw' 事件记在 game_events。
-- 打第一张牌时庄家手上是 14 张，与任务书描述一致，只是记录的分界点不同。
CREATE TABLE game_initial_hands (
    game_id      BIGINT   NOT NULL,
    round_index  SMALLINT NOT NULL,
    seat         SMALLINT NOT NULL,
    tiles        TEXT     NOT NULL,         -- 紧凑记法，四家各 13 张

    PRIMARY KEY (game_id, round_index, seat),
    FOREIGN KEY (game_id, round_index)
        REFERENCES game_rounds (game_id, round_index) ON DELETE CASCADE
);

-- 每个小局的牌山（任务书 §6 要求可查「牌山」）
--
-- 这里存的不只是「服务器说的 136 张牌」，而是一份**可被玩家独立核验**的记录。
-- 除了牌山本身，还要存下生成它的种子与开赛前的承诺值，任何人拿这些数据
-- 就能自己重放一遍洗牌，确认牌山没有被操控。
-- 协议细节见 docs/牌山可验证性.md。
CREATE TABLE game_walls (
    game_id     BIGINT   NOT NULL,
    round_index SMALLINT NOT NULL,

    -- 按摸牌顺序逐张写成的紧凑记法（不是按花色分组），人工核对时可直接看懂。
    -- 注意：验证时不要用这个字段重算哈希，它的长度和顺序都与洗牌输入不同。
    tiles       TEXT     NOT NULL,

    -- 牌山指纹：SHA-256(136 张牌的原始字节)。核验时先比它，比逐张比对快得多。
    wall_hash   BYTEA    NOT NULL,

    -- 可验证性所需的材料
    server_seed   BYTEA,        -- 本场游戏的服务器种子，牌局结束后公布
    commitment    BYTEA,        -- 开赛前公布并入库；事后用它证明种子没被替换
    client_seeds  BYTEA[],      -- 四位玩家按座位顺序提交的种子，顺序不可乱

    -- 洗牌实现依赖的运行时版本。万一 Go 改变了库函数行为，
    -- 至少知道该用哪个版本去验证历史牌局。
    go_version  TEXT,

    -- 宝牌指示牌（杠后可能追加，故用数组）。立直和牌时里宝牌也要记，
    -- 否则无法复核算分结果。
    dora_indicators  TEXT[] NOT NULL DEFAULT '{}',
    ura_indicators   TEXT[] NOT NULL DEFAULT '{}',

    PRIMARY KEY (game_id, round_index),
    FOREIGN KEY (game_id, round_index)
        REFERENCES game_rounds (game_id, round_index) ON DELETE CASCADE
);

-- 承诺值必须在开赛前写入，不能等牌局结束一起写——事后补写的承诺等于没有承诺。
-- 因此这个字段在牌局进行中就已非空，只有 server_seed 是结束后才填。
COMMENT ON COLUMN game_walls.commitment IS
    '开赛前公布的承诺值 SHA256(域分隔 || gameID || serverSeed)，必须先于牌局入库';
COMMENT ON COLUMN game_walls.server_seed IS
    '牌局结束后公布；与 commitment 比对即可确认种子未被事后替换';
COMMENT ON COLUMN game_walls.tiles IS
    '按摸牌顺序的紧凑记法，供人工核对；重算哈希请用 136 张牌的原始字节';

-- ── 和牌结算 ──────────────────────────────────────────────────────

-- 每次和牌的算分明细。一次和牌一行；一炮多响时会有多行。
CREATE TABLE game_wins (
    game_id       BIGINT   NOT NULL,
    round_index   SMALLINT NOT NULL,
    win_seq       SMALLINT NOT NULL,        -- 同一小局内第几次和牌，从 1 开始
    winner_seat   SMALLINT NOT NULL,
    loser_seat    SMALLINT,                 -- 放铳者；自摸时为 NULL
    is_tsumo      BOOLEAN  NOT NULL,
    winning_tile  TEXT     NOT NULL,

    -- 算分结果。翻数与符数都存下来，便于核对是否符合算分文档。
    han           SMALLINT NOT NULL,
    fu            SMALLINT NOT NULL,
    yakuman       SMALLINT NOT NULL DEFAULT 0,   -- 役满倍数，0 表示非役满
    -- 命中的役种明细，形如 [{"name":"立直","han":1}, ...]
    yaku          JSONB    NOT NULL DEFAULT '[]'::jsonb,
    -- 宝牌构成，形如 {"dora":2,"ura":1,"aka":1}
    dora          JSONB    NOT NULL DEFAULT '{}'::jsonb,
    -- 四个座位各自的收支，已含本场棒；形如 {"0":-1000,"1":-1000,"2":-1000,"3":3900}
    payments      JSONB    NOT NULL DEFAULT '{}'::jsonb,

    PRIMARY KEY (game_id, round_index, win_seq),
    FOREIGN KEY (game_id, round_index)
        REFERENCES game_rounds (game_id, round_index) ON DELETE CASCADE
);

-- ── 动作流水 ──────────────────────────────────────────────────────

-- 每一回合的动作（任务书 §6 要求可查「每一回合的摸牌 & 出牌」）
CREATE TABLE game_events (
    game_id     BIGINT   NOT NULL,
    round_index SMALLINT NOT NULL,
    seq         INTEGER  NOT NULL,          -- 小局内的动作序号，从 1 开始
    seat        SMALLINT NOT NULL,
    action      TEXT     NOT NULL,
    tile        TEXT,                       -- 无牌的动作（如立直）为 NULL
    -- 动作附带的上下文，例如鸣牌的种类、是否暗杠
    detail      JSONB    NOT NULL DEFAULT '{}'::jsonb,

    PRIMARY KEY (game_id, round_index, seq),
    CONSTRAINT game_events_action CHECK (action IN (
        'draw',        -- 摸牌
        'discard',     -- 打牌
        'chi',         -- 吃
        'pon',         -- 碰
        'kan',         -- 杠（明杠/暗杠/加杠，种类见 detail）
        'riichi',      -- 立直宣言
        'riichi_stick',-- 支付立直棒
        'ippatsu',     -- 一发成立标记
        'tsumo',       -- 自摸和牌
        'ron',         -- 荣和
        'ryuukyoku'    -- 流局
    )),
    FOREIGN KEY (game_id, round_index)
        REFERENCES game_rounds (game_id, round_index) ON DELETE CASCADE
);

-- 回放某一小局时按 seq 顺序取全部动作
CREATE INDEX game_events_replay_idx ON game_events (game_id, round_index, seq);
-- 按玩家查他打过的动作（数据统计用）
CREATE INDEX game_events_seat_idx ON game_events (game_id, round_index, seat);

COMMIT;
