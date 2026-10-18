import { useCallback, useEffect, useRef } from "react";
import type { ChatMessage } from "../chat/types";
import { MessageView } from "./MessageView";

interface ConversationProps {
    messages: ChatMessage[];
    configured: boolean;
}

export function Conversation({ messages, configured }: ConversationProps) {
    const viewport = useRef<HTMLDivElement>(null);
    const followOutput = useRef(true);
    const frame = useRef<number | null>(null);

    const handleScroll = useCallback(() => {
        const element = viewport.current;
        if (element === null) {
            return;
        }
        followOutput.current = element.scrollHeight - element.scrollTop - element.clientHeight < 72;
    }, []);

    // 流式回答每个增量都会换一次 messages，这里把滚动压到每帧一次，
    // 否则每个 token 都要读一次 scrollHeight，长回答会明显掉帧。
    useEffect(() => {
        if (!followOutput.current || frame.current !== null) {
            return;
        }

        frame.current = window.requestAnimationFrame(() => {
            frame.current = null;

            const element = viewport.current;
            if (element !== null && followOutput.current) {
                element.scrollTop = element.scrollHeight;
            }
        });

        return () => {
            if (frame.current !== null) {
                window.cancelAnimationFrame(frame.current);
                frame.current = null;
            }
        };
    }, [messages]);

    return (
        <div className="conversation" ref={viewport} onScroll={handleScroll}>
            <div className="conversation-inner">
                {messages.length === 0 ? (
                    <section className="welcome">
                        <div className="welcome-orbit" aria-hidden="true">
                            <span className="orbit-ring" />
                            <span className="orbit-core" />
                        </div>
                        <p className="eyebrow">NEUTRON WORKSPACE</p>
                        <h1>{configured ? "从一个问题开始。" : "先连接一个模型。"}</h1>
                        <p>
                            {configured
                                ? "支持 Markdown、代码块与流式回答。Agent 能力会从这里继续生长。"
                                : "完成模型连接配置后，这里会成为你的 Agent 工作区。"}
                        </p>
                    </section>
                ) : (
                    messages.map((message) => <MessageView key={message.id} message={message} />)
                )}
            </div>
        </div>
    );
}
