-- 牌局历史的补列（阶段五）。
--
-- 两处都是**老表缺列**，不是设计变更。

-- ── 承诺值：按场，不按小局 ────────────────────────────────────────────
--
-- 承诺是一整场一件事（Commit(gameID, serverSeed) 一次算一场），而 game_walls 是
-- **按小局**一行，还要外键到 game_rounds —— 那张表在小局结束前插不进去
-- （ended_by NOT NULL，开局时还不知道这一局怎么结束）。
--
-- 所以要满足「承诺先于发牌入库」，承诺必须挂在 games 上：开局那一事务只写
-- games + game_seats，此时发牌还没发生。
--
-- game_walls.commitment 照旧在牌局结束时填同一个值，让每一行自包含
-- （/api/games/{id} 读一行就能拿出全部核验材料，不用回头 join games）。
ALTER TABLE games
    ADD COLUMN commitment   BYTEA,
    ADD COLUMN committed_at TIMESTAMPTZ;

COMMENT ON COLUMN games.commitment IS
    '开赛前写入并公布的承诺值 SHA256(域分隔 || gameID || serverSeed)；NULL 表示这一场没有承诺';
COMMENT ON COLUMN games.committed_at IS
    '承诺入库的时刻。必须早于该场第一次洗牌，否则这个承诺没有意义';

-- ── 帕奥责任者：history 详情要能解释那笔独付 ──────────────────────────
--
-- M-League 有责任払い（大三元 / 大四喜 / 四槓子，见规则集 §8.5）。有包时
-- payments 里会出现某一座独自承担全额的大额支出；少了这两列，复盘页面
-- 只能把它显示成「算错了」。协议 §6.8 的 roundEnd 本来就有这两个字段。
ALTER TABLE game_wins
    ADD COLUMN pao_seat SMALLINT,
    ADD COLUMN pao_yaku TEXT;

COMMENT ON COLUMN game_wins.pao_seat IS
    '帕奥责任者座位；无包时为 NULL（注意与自摸的 loser_seat 不是一回事）';
