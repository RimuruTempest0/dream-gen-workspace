import { useEffect, useRef, useState, type Ref } from "react";
import { Alert, Button, Drawer, Segmented } from "antd";
import { ArrowRightOutlined, LoadingOutlined } from "@ant-design/icons";
import {
  call,
  errorText,
  type Concept,
  type ConceptLibrary,
  type ConceptInput,
  type ConceptResult,
} from "./api";
import { useData } from "./ui";
import { ConceptGlyph } from "./ConceptGlyphs";

// ---- concept stage (0.7.18) ---------------------------------------------------
// A form and one to three stances; the backend's LLM writes them into the prompt.

const strengths = [
  { value: 0, label: "点缀" },
  { value: 1, label: "平衡" },
  { value: 2, label: "主导" },
];

// "巨物 × 浪漫主义 / 极简主义" for the labels of one compose.
export function conceptSummary(labels: string[]) {
  if (!labels.length) return "";
  const [form] = labels[0].split(" × ");
  return form + " × " + labels.map((l) => l.split(" × ")[1] || l).join(" / ");
}

// Below the prompt box: the entry to the stage, or the concept now in the prompt.
export function ConceptBar({
  labels,
  canUndo,
  disabled,
  onOpen,
  onClear,
  onUndo,
  openRef,
}: {
  labels: string[];
  canUndo: boolean;
  disabled?: boolean;
  onOpen: () => void;
  onClear: () => void;
  onUndo: () => void;
  openRef: Ref<HTMLButtonElement>;
}) {
  if (!labels.length)
    return (
      <div className="concept-bar">
        <button
          ref={openRef}
          type="button"
          className="concept-entry"
          onClick={onOpen}
          disabled={disabled}
          aria-haspopup="dialog"
        >
          <span className="concept-entry-mark" aria-hidden="true">
            <ConceptGlyph id="vault" />
          </span>
          <span className="concept-entry-copy">
            <strong>概念</strong>
            <span>给画面一个形体和立场</span>
          </span>
          <ArrowRightOutlined aria-hidden="true" />
        </button>
        {canUndo && (
          <Button type="link" size="small" onClick={onUndo}>
            撤销改写
          </Button>
        )}
      </div>
    );
  return (
    <div className="concept-bar concept-bar-set">
      <span className="concept-entry-mark" aria-hidden="true">
        <ConceptGlyph id="vault" />
      </span>
      <span className="concept-current">
        <span className="concept-current-kicker">概念</span>
        <strong>{conceptSummary(labels)}</strong>
      </span>
      <span className="concept-bar-actions">
        {canUndo && (
          <Button type="link" size="small" onClick={onUndo}>
            撤销改写
          </Button>
        )}
        <Button
          ref={openRef}
          size="small"
          onClick={onOpen}
          disabled={disabled}
          aria-haspopup="dialog"
        >
          更换
        </Button>
        <Button size="small" onClick={onClear}>
          清除
        </Button>
      </span>
    </div>
  );
}

function ConceptCard({
  concept,
  selected,
  order,
  onPick,
}: {
  concept: Concept;
  selected: boolean;
  order?: number;
  onPick: () => void;
}) {
  return (
    <button
      type="button"
      className="concept-card"
      aria-pressed={selected}
      onClick={onPick}
      title={concept.look}
    >
      <ConceptGlyph id={concept.id} />
      <span className="concept-card-name">{concept.name}</span>
      <span className="concept-card-en">{concept.en}</span>
      {selected && order !== undefined && (
        <span className="concept-card-order" aria-hidden="true">
          {order}
        </span>
      )}
    </button>
  );
}

function groupsOf(list: Concept[]) {
  const out: { group: string; items: Concept[] }[] = [];
  for (const c of list) {
    const g = out.find((x) => x.group === c.group);
    if (g) g.items.push(c);
    else out.push({ group: c.group, items: [c] });
  }
  return out;
}

export function ConceptStage({
  open,
  hasReferences,
  onClose,
  onAfterClose,
  prepare,
  onComposed,
}: {
  open: boolean;
  hasReferences: boolean;
  onClose: () => void;
  // Called once the drawer has closed; applied tells whether the prompt changed.
  onAfterClose: (applied: boolean) => void;
  // Returns the prompt to rewrite for n stances, or null when the user backs out.
  prepare: (n: number) => Promise<string | null>;
  onComposed: (result: ConceptResult) => void;
}) {
  const library = useData<ConceptLibrary>(["concepts"], "ListConcepts");
  const [form, setForm] = useState("");
  const [stances, setStances] = useState<string[]>([]);
  const [strength, setStrength] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const applied = useRef(false);
  // With reference images the concept stays an accent unless the user chose otherwise.
  const level = strength ?? (hasReferences ? 0 : 1);
  useEffect(() => {
    if (open) {
      applied.current = false;
      setError("");
    }
  }, [open]);
  const lib = library.data;
  const formCard = lib?.forms.find((c) => c.id === form);
  const stanceCards = stances
    .map((id) => lib?.stances.find((c) => c.id === id))
    .filter((c): c is Concept => !!c);
  const ready = !!formCard && stanceCards.length > 0;
  function pickStance(id: string) {
    setError("");
    setStances((v) => {
      if (v.includes(id)) return v.length > 1 ? v.filter((x) => x !== id) : v;
      // A fourth choice replaces the earliest one.
      return [...(v.length >= 3 ? v.slice(1) : v), id];
    });
  }
  function random() {
    if (!lib) return;
    const f = lib.forms[Math.floor(Math.random() * lib.forms.length)];
    const s = lib.stances[Math.floor(Math.random() * lib.stances.length)];
    setForm(f.id);
    setStances([s.id]);
    setError("");
  }
  async function compose() {
    if (!ready || busy) return;
    setError("");
    const prompt = await prepare(stanceCards.length);
    if (prompt === null) return;
    setBusy(true);
    try {
      const input: ConceptInput = {
        prompt,
        form,
        stances,
        strength: level,
        hasReferences,
      };
      const result = await call<ConceptResult>("ComposeConcept", input);
      applied.current = true;
      onComposed(result);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  const selectionKey = form + "|" + stances.join(",");
  const title =
    ready && formCard ? (
      <>
        {formCard.name}
        <span className="concept-times" aria-hidden="true">
          ×
        </span>
        <span className="sr-only">与</span>
        {stanceCards.map((c) => c.name).join(" / ")}
      </>
    ) : formCard ? (
      <>
        {formCard.name}
        <span className="concept-times" aria-hidden="true">
          ×
        </span>
        <span className="concept-placeholder">选一个立场</span>
      </>
    ) : (
      <span className="concept-placeholder">选一个形体，再选一到三个立场</span>
    );
  const footer = (
    <div className="concept-footer">
      <div className="concept-summary" aria-live="polite">
        <h3 className="concept-summary-title">{title}</h3>
        <ul className="concept-looks" key={selectionKey}>
          {formCard && (
            <li>
              <span>{formCard.name}</span>
              {formCard.look}
            </li>
          )}
          {stanceCards.map((c) => (
            <li key={c.id}>
              <span>{c.name}</span>
              {c.look}
            </li>
          ))}
        </ul>
      </div>
      {error && (
        <Alert
          type="error"
          showIcon
          message="改写没有成功，提示词未改动"
          description={error}
        />
      )}
      <div className="concept-actions">
        <div className="concept-strength">
          <span id="concept-strength-label">概念强度</span>
          <Segmented
            aria-labelledby="concept-strength-label"
            options={strengths}
            value={level}
            onChange={(v) => setStrength(Number(v))}
          />
        </div>
        {/* Not the loading prop: its spinner can stay half-faded in the
            hidden drawer and then joins the button's accessible name. */}
        <Button
          type="primary"
          size="large"
          icon={busy ? <LoadingOutlined aria-hidden="true" /> : undefined}
          aria-busy={busy}
          disabled={!ready || busy}
          onClick={compose}
        >
          {busy
            ? "正在改写…"
            : stanceCards.length > 1
              ? `生成 ${stanceCards.length} 条对比提示词`
              : "融入提示词"}
        </Button>
      </div>
      <p className="concept-note">
        由 AI 模型改写提示词，不生成图片、不产生生图费用；未配置 AI 模型
        时直接附加概念要点。
      </p>
    </div>
  );
  return (
    <Drawer
      open={open}
      onClose={onClose}
      afterOpenChange={(v) => {
        if (!v) onAfterClose(applied.current);
      }}
      placement="right"
      width={720}
      rootClassName="concept-stage"
      aria-label="概念台"
      title={
        <div className="concept-stage-heading">
          <span className="concept-stage-title">概念台</span>
          <span className="concept-stage-sub">
            Concept stage · 选一个形体，配一到三个立场
          </span>
        </div>
      }
      footer={footer}
    >
      {library.error ? (
        <Alert
          type="error"
          message="概念库读取失败"
          description={errorText(library.error)}
        />
      ) : !lib ? (
        <p className="concept-loading">正在读取概念库</p>
      ) : (
        <>
          <section className="concept-section" aria-labelledby="concept-picks">
            <h3 id="concept-picks" className="concept-section-title">
              精选组合
            </h3>
            <div className="concept-picks">
              {lib.picks.map((p) => {
                const f = lib.forms.find((c) => c.id === p.form);
                const s = lib.stances.find((c) => c.id === p.stance);
                const on =
                  form === p.form &&
                  stances.length === 1 &&
                  stances[0] === p.stance;
                return (
                  <button
                    key={p.name}
                    type="button"
                    className="concept-pick"
                    aria-pressed={on}
                    onClick={() => {
                      setForm(p.form);
                      setStances([p.stance]);
                      setError("");
                    }}
                  >
                    <strong>{p.name}</strong>
                    <span>
                      {f?.name} × {s?.name}
                    </span>
                  </button>
                );
              })}
              <button
                type="button"
                className="concept-pick concept-random"
                onClick={random}
              >
                <strong>随机一组</strong>
                <span>换个思路</span>
              </button>
            </div>
          </section>
          <div className="concept-columns">
            <section
              className="concept-section"
              aria-labelledby="concept-forms"
            >
              <h3 id="concept-forms" className="concept-section-title">
                形体 <span>选 1 个</span>
              </h3>
              {groupsOf(lib.forms).map((g) => (
                <div
                  className="concept-group"
                  key={g.group}
                  role="group"
                  aria-label={"形体 · " + g.group}
                >
                  <span className="concept-group-name" aria-hidden="true">
                    {g.group}
                  </span>
                  <div className="concept-cards">
                    {g.items.map((c) => (
                      <ConceptCard
                        key={c.id}
                        concept={c}
                        selected={form === c.id}
                        onPick={() => {
                          setForm(c.id);
                          setError("");
                        }}
                      />
                    ))}
                  </div>
                </div>
              ))}
            </section>
            <section
              className="concept-section"
              aria-labelledby="concept-stances"
            >
              <h3 id="concept-stances" className="concept-section-title">
                立场 <span>最多 3 个</span>
              </h3>
              {groupsOf(lib.stances).map((g) => (
                <div
                  className="concept-group"
                  key={g.group}
                  role="group"
                  aria-label={"立场 · " + g.group}
                >
                  <span className="concept-group-name" aria-hidden="true">
                    {g.group}
                  </span>
                  <div className="concept-cards">
                    {g.items.map((c) => (
                      <ConceptCard
                        key={c.id}
                        concept={c}
                        selected={stances.includes(c.id)}
                        order={
                          stances.length > 1
                            ? stances.indexOf(c.id) + 1
                            : undefined
                        }
                        onPick={() => pickStance(c.id)}
                      />
                    ))}
                  </div>
                </div>
              ))}
            </section>
          </div>
        </>
      )}
    </Drawer>
  );
}
