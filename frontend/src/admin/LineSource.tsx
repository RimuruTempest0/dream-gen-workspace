import { Alert } from "antd";
import { time, useData } from "../ui";
import type { LineSource } from "./types";

// Which commit a build of a product line would take now. The admin tool builds
// each line in its own build folder from the newest commit of the line's branch;
// the source folder is only read.
export function useLineSource(line: "ecommerce" | "creative") {
  return useData<LineSource>(["line-source", line], "LineSource", [line], 5000);
}

export function LineFacts({ src }: { src?: LineSource }) {
  return (
    <>
      <dl className="admin-facts">
        <dt>版本号</dt>
        <dd>{src?.version || "—"}</dd>
        <dt>分支</dt>
        <dd>{src?.branch || "—"}</dd>
        <dt>将要构建的提交</dt>
        <dd>
          {src?.commit ? (
            <>
              <code>{src.commit}</code> {src.subject}
              {src.at && <span className="hint"> · {time(src.at)}</span>}
            </>
          ) : (
            "—"
          )}
        </dd>
      </dl>
      {src?.blocked && <Alert type="warning" showIcon message={src.blocked} />}
      {!!src?.uncommitted && (
        <Alert
          type="info"
          showIcon
          message={`源码目录有 ${src.uncommitted} 个未提交的改动，不会进入这次构建。`}
        />
      )}
    </>
  );
}
