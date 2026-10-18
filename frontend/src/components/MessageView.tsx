import { memo, useState } from "react";
import type { ChatMessage } from "../chat/types";
import { MarkdownRenderer } from "./MarkdownRenderer";

interface MessageViewProps {
    message: ChatMessage;
}

function SparkIcon() {
    return (
        <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M12 2.7 13.7 8l5.3 1.7-5.3 1.7L12 16.7l-1.7-5.3L5 9.7 10.3 8 12 2.7Z" />
            <path d="m18.2 15.2.8 2.5 2.5.8-2.5.8-.8 2.5-.8-2.5-2.5-.8 2.5-.8.8-2.5Z" />
        </svg>
    );
}

function ReasoningPart({ text }: { text: string }) {
    const [open, setOpen] = useState(false);

    return (
        <details className="reasoning" open={open} onToggle={(event) => setOpen(event.currentTarget.open)}>
            <summary>
                <span className="reasoning-pulse" />
                思考过程
                <span className="reasoning-chevron">{open ? "−" : "+"}</span>
            </summary>
            <div className="reasoning-body">{text}</div>
        </details>
    );
}

export const MessageView = memo(function MessageView({ message }: MessageViewProps) {
    const assistant = message.role === "assistant";

    return (
        <article className={`message message-${message.role}`}>
            <div className="message-gutter" aria-hidden="true">
                {assistant ? (
                    <span className="assistant-mark">
                        <SparkIcon />
                    </span>
                ) : (
                    <span className="user-mark">你</span>
                )}
            </div>

            <div className="message-content">
                <div className="message-label">{assistant ? "Neutron" : "You"}</div>

                {message.parts.map((part) => {
                    switch (part.type) {
                        case "text":
                            return part.text === "" ? null : (
                                <MarkdownRenderer
                                    key={`${part.type}-${part.index}`}
                                    content={part.text}
                                    streaming={assistant && message.status === "streaming"}
                                />
                            );

                        case "reasoning":
                            return (
                                <ReasoningPart key={`${part.type}-${part.index}`} text={part.text} />
                            );

                        case "tool-call":
                            return (
                                <div className="tool-call" key={`${part.type}-${part.index}`}>
                                    <span className="tool-call-dot" />
                                    请求调用 <code>{part.name}</code>
                                </div>
                            );

                        case "notice":
                            return (
                                <div
                                    className={`message-notice is-${part.tone}`}
                                    key={`${part.type}-${part.index}`}
                                    role={part.tone === "error" ? "alert" : undefined}
                                >
                                    {part.text}
                                </div>
                            );
                    }
                })}

                {assistant && message.status === "pending" && (
                    <div className="thinking-indicator" aria-label="正在思考">
                        <span />
                        <span />
                        <span />
                    </div>
                )}
            </div>
        </article>
    );
});
