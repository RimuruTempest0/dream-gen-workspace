import { App as AntApp, Dropdown, Radio, Tooltip } from "antd";
import { GlobalOutlined } from "@ant-design/icons";
import { t, useI18n, type Lang } from ".";

// Each language is named in its own script and tagged with its language, so
// a customer who cannot read the current one still finds theirs and screen
// readers say it right.
function useSwitch() {
  const { lang, info, setLanguage, languages } = useI18n();
  const { message } = AntApp.useApp();
  const choose = (next: Lang) => {
    if (next === lang) return;
    setLanguage(next).catch((e) =>
      message.error(
        `${t("shell.language.failed")}: ${e instanceof Error ? e.message : e}`,
      ),
    );
  };
  return { lang, info, languages, choose };
}

/** Language switch at the foot of the sidebar. */
export function RailLanguageSwitch({ tooltip }: { tooltip: boolean }) {
  const { lang, info, languages, choose } = useSwitch();
  const name = t("shell.language.current", { language: info.native });
  return (
    <Dropdown
      trigger={["click"]}
      placement="topLeft"
      menu={{
        selectable: true,
        selectedKeys: [lang],
        items: languages.map((l) => ({
          key: l.code,
          label: <span lang={l.code}>{l.native}</span>,
        })),
        onClick: ({ key }) => choose(key as Lang),
      }}
    >
      <Tooltip title={tooltip ? name : undefined} placement="right">
        <button
          type="button"
          className="rail-item rail-language"
          aria-label={name}
        >
          <GlobalOutlined aria-hidden="true" />
          <span className="rail-label" lang={lang}>
            {info.native}
          </span>
        </button>
      </Tooltip>
    </Dropdown>
  );
}

/** Language section of the settings page. */
export function LanguagePanel() {
  const { lang, languages, choose } = useSwitch();
  return (
    <section className="panel language-panel">
      <div className="panel-heading">
        <h2 id="language-heading">
          <GlobalOutlined aria-hidden="true" /> {t("shell.language.label")}
        </h2>
      </div>
      <div role="radiogroup" aria-labelledby="language-heading">
        <Radio.Group
          optionType="button"
          value={lang}
          onChange={(e) => choose(e.target.value as Lang)}
          options={languages.map((l) => ({
            value: l.code,
            label: <span lang={l.code}>{l.native}</span>,
          }))}
        />
      </div>
      <p className="hint">{t("shell.language.hint")}</p>
    </section>
  );
}
