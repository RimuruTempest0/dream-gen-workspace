// Puts React elements (links, bold numbers) into a translated sentence.
// t() leaves placeholders it was not given untouched, so
//   fill(t("license.notice.trialUsedBody", { limit: 50 }), { link: <Link …/> })
// keeps the word order of each language and places the element where the
// language puts {link}.
import { Fragment, type ReactNode } from "react";

export function fill(text: string, nodes: Record<string, ReactNode>) {
  const parts = text.split(/\{(\w+)\}/);
  return (
    <>
      {parts.map((p, i) =>
        i % 2 === 0 ? (
          p
        ) : (
          <Fragment key={i}>{p in nodes ? nodes[p] : `{${p}}`}</Fragment>
        ),
      )}
    </>
  );
}
