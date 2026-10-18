import { memo, useCallback, useState, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

interface MarkdownRendererProps {
    content: string;
    streaming?: boolean;
}

interface CodeProps {
    children?: ReactNode;
    className?: string;
}

function Code({ children, className }: CodeProps) {
    const [copied, setCopied] = useState(false);
    const text = String(children ?? "").replace(/\n$/, "");
    const language = className?.replace(/^language-/, "") ?? "text";
    const block = text.includes("\n") || className !== undefined;

    const copy = useCallback(() => {
        void navigator.clipboard.writeText(text).then(() => {
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1400);
        });
    }, [text]);

    if (!block) {
        return <code className="markdown-inline-code">{children}</code>;
    }

    return (
        <div className="markdown-code-block">
            <div className="markdown-code-header">
                <span>{language}</span>
                <button type="button" onClick={copy} aria-label="复制代码">
                    {copied ? "已复制" : "复制"}
                </button>
            </div>
            <pre>
                <code>{text}</code>
            </pre>
        </div>
    );
}

const components: Components = {
    code: Code,
    // 只转发真正属于 <a> 的属性：react-markdown 会把 AST 的 node 一起传进来，
    // 整个展开就会变成 DOM 上的 node="[object Object]"。
    a: ({ children, href, title }) => (
        <a href={href} title={title} target="_blank" rel="noreferrer">
            {children}
        </a>
    ),
};

export const MarkdownRenderer = memo(function MarkdownRenderer({
    content,
    streaming = false,
}: MarkdownRendererProps) {
    return (
        <div className={`markdown${streaming ? " is-streaming" : ""}`}>
            <ReactMarkdown remarkPlugins={[remarkGfm]} components={components} skipHtml>
                {content}
            </ReactMarkdown>
            {streaming && <span className="stream-caret" aria-hidden="true" />}
        </div>
    );
});
