import { useCallback, useEffect, useRef, useState } from "react";
import { Events } from "@wailsio/runtime";
import {
    ChatService,
    type ChatDelta,
    type ChatOutcome,
    type ChatStatus,
    type ChatTurn,
} from "../../bindings/neutron-star";
import { messageText, type ChatMessage, type MessagePart } from "./types";

interface ChatController {
    status: ChatStatus | null;
    messages: ChatMessage[];
    running: boolean;
    problem: string | null;
    send: (prompt: string) => Promise<boolean>;
    stop: () => void;
    clear: () => void;
    reload: () => void;
}

/**
 * 停止原因的中文说明。end_turn 是正常结束，不在此表里，
 * 其余原因直接显示原始枚举对用户没有意义。
 */
const STOP_REASON_LABELS: Record<string, string> = {
    max_tokens: "回答达到长度上限，已截断",
    stop_sequence: "命中停止序列",
    tool_use: "模型请求调用工具，当前版本还不会执行",
    refusal: "模型拒绝继续",
    pause_turn: "生成被暂停，可以继续追问",
    context_window_exceeded: "超出上下文窗口",
    other: "生成提前结束",
};

let nextID = 0;

function createID(prefix: string): string {
    nextID += 1;
    return `${prefix}-${Date.now().toString(36)}-${nextID.toString(36)}`;
}

function updateMessage(
    messages: ChatMessage[],
    id: string | null,
    transform: (message: ChatMessage) => ChatMessage,
): ChatMessage[] {
    if (id === null) {
        return messages;
    }

    return messages.map((message) => (message.id === id ? transform(message) : message));
}

function nextIndex(parts: MessagePart[]): number {
    return parts.length === 0 ? 0 : Math.max(...parts.map((part) => part.index)) + 1;
}

function appendDelta(parts: MessagePart[], delta: ChatDelta): MessagePart[] {
    const index = delta.partIndex;
    const current = parts.find((part) => part.index === index);
    let next: MessagePart;

    switch (delta.kind) {
        case "reasoning":
            next = {
                type: "reasoning",
                index,
                text: current?.type === "reasoning" ? current.text + (delta.text ?? "") : (delta.text ?? ""),
            };
            break;

        case "tool_call":
            next = {
                type: "tool-call",
                index,
                name: delta.toolName || (current?.type === "tool-call" ? current.name : "未命名工具"),
            };
            break;

        case "refusal":
            next = {
                type: "notice",
                index,
                text:
                    current?.type === "notice"
                        ? current.text + (delta.text ?? "")
                        : (delta.text ?? "模型拒绝了这项请求"),
                tone: "error",
            };
            break;

        default:
            next = {
                type: "text",
                index,
                text: current?.type === "text" ? current.text + (delta.text ?? "") : (delta.text ?? ""),
            };
    }

    return [...parts.filter((part) => part.index !== index), next].sort((a, b) => a.index - b.index);
}

function reconcileFinalText(parts: MessagePart[], text: string): MessagePart[] {
    const firstText = parts.find((part) => part.type === "text");
    const index = firstText?.index ?? nextIndex(parts);
    const next: MessagePart = { type: "text", index, text };

    return [...parts.filter((part) => part.type !== "text"), next].sort((a, b) => a.index - b.index);
}

function appendNotice(parts: MessagePart[], text: string, tone: "info" | "error"): MessagePart[] {
    const next = [...parts, { type: "notice" as const, index: nextIndex(parts), text, tone }];

    return next.sort((a, b) => a.index - b.index);
}

/** toHistory 把界面上的消息压成后端可重放的历史：只有正文，没有提示与工具。 */
function toHistory(messages: ChatMessage[]): ChatTurn[] {
    const turns: ChatTurn[] = [];

    for (const message of messages) {
        const text = messageText(message).trim();
        if (text !== "") {
            turns.push({ role: message.role, text });
        }
    }

    return turns;
}

function errorText(error: unknown): string {
    return error instanceof Error ? error.message : String(error);
}

export function useChatService(): ChatController {
    const [status, setStatus] = useState<ChatStatus | null>(null);
    const [messages, setMessages] = useState<ChatMessage[]>([]);
    const [running, setRunning] = useState(false);
    const [problem, setProblem] = useState<string | null>(null);

    const activeAssistantID = useRef<string | null>(null);

    // messagesRef 给 send 读历史用：读的是已提交的状态，
    // 不依赖闭包里那份可能过期的 messages。
    const messagesRef = useRef<ChatMessage[]>([]);

    // 流式增量先攒在 buffer 里，每帧最多提交一次：既避免逐 token 重解析
    // Markdown，也避免事件早于 React 提交时把首段内容丢掉。
    const buffer = useRef<{ id: string; parts: MessagePart[] } | null>(null);
    const frame = useRef<number | null>(null);

    useEffect(() => {
        messagesRef.current = messages;
    }, [messages]);

    const cancelFlush = useCallback(() => {
        if (frame.current !== null) {
            window.cancelAnimationFrame(frame.current);
            frame.current = null;
        }
    }, []);

    const flush = useCallback(() => {
        frame.current = null;

        const pending = buffer.current;
        if (pending === null) {
            return;
        }

        const { id, parts } = pending;
        setMessages((current) =>
            updateMessage(current, id, (message) => ({ ...message, status: "streaming", parts })),
        );
    }, []);

    const scheduleFlush = useCallback(() => {
        if (frame.current === null) {
            frame.current = window.requestAnimationFrame(flush);
        }
    }, [flush]);

    useEffect(() => {
        const offDelta = Events.On("chat:delta", (event) => {
            const delta = event.data as ChatDelta;
            const id = activeAssistantID.current;
            if (id === null) {
                return;
            }

            const pending = buffer.current;
            if (pending === null || pending.id !== id) {
                buffer.current = { id, parts: appendDelta([], delta) };
            } else {
                pending.parts = appendDelta(pending.parts, delta);
            }

            scheduleFlush();
        });

        const offDone = Events.On("chat:done", (event) => {
            const outcome = event.data as ChatOutcome;
            const id = activeAssistantID.current;
            const pending = buffer.current;

            cancelFlush();
            buffer.current = null;
            activeAssistantID.current = null;
            setRunning(false);

            if (id === null) {
                return;
            }

            setMessages((current) =>
                updateMessage(current, id, (message) => {
                    const source = pending?.id === id ? pending.parts : message.parts;
                    let parts = reconcileFinalText(source, outcome.text ?? "");

                    const stopReason = outcome.stopReason;
                    if (stopReason && stopReason !== "end_turn") {
                        parts = appendNotice(
                            parts,
                            STOP_REASON_LABELS[stopReason] ?? `停止原因：${stopReason}`,
                            "info",
                        );
                    }

                    return { ...message, status: "complete", parts };
                }),
            );
        });

        const offError = Events.On("chat:error", (event) => {
            const outcome = event.data as ChatOutcome;
            const id = activeAssistantID.current;
            const pending = buffer.current;

            cancelFlush();
            buffer.current = null;
            activeAssistantID.current = null;
            setRunning(false);

            if (id === null) {
                return;
            }

            const canceled = outcome.canceled === true;
            const detail = outcome.error ?? "生成失败";

            if (!canceled) {
                setProblem(detail);
            }

            setMessages((current) =>
                updateMessage(current, id, (message) => {
                    const source = pending?.id === id ? pending.parts : message.parts;
                    let parts = reconcileFinalText(source, outcome.text ?? "");

                    // 用户按了停止：这不是失败，不该出现红色报错，
                    // 也不该给后续发送留下一个"问题"状态。
                    if (canceled) {
                        parts = appendNotice(parts, "已停止生成", "info");

                        return { ...message, status: "complete", parts };
                    }

                    parts = appendNotice(parts, detail, "error");

                    return { ...message, status: "error", parts };
                }),
            );
        });

        return () => {
            cancelFlush();
            offDelta();
            offDone();
            offError();
        };
    }, [cancelFlush, scheduleFlush]);

    const applyStatus = useCallback((next: ChatStatus) => {
        setStatus(next);
        setRunning(next.running);
    }, []);

    useEffect(() => {
        void ChatService.Status()
            .then((next) => {
                applyStatus(next);
            })
            .catch((error: unknown) => setProblem(errorText(error)));
    }, [applyStatus]);

    const send = useCallback(
        async (prompt: string): Promise<boolean> => {
            const content = prompt.trim();
            if (content === "" || running) {
                return false;
            }

            const history = toHistory(messagesRef.current);
            const user: ChatMessage = {
                id: createID("user"),
                role: "user",
                status: "complete",
                parts: [{ type: "text", index: 0, text: content }],
            };
            const assistant: ChatMessage = {
                id: createID("assistant"),
                role: "assistant",
                status: "pending",
                parts: [],
            };

            activeAssistantID.current = assistant.id;
            buffer.current = null;
            setMessages((current) => [...current, user, assistant]);
            setProblem(null);
            setRunning(true);

            try {
                await ChatService.Send(content, history);
                return true;
            } catch (error: unknown) {
                const detail = errorText(error);
                setMessages((current) =>
                    updateMessage(current, assistant.id, (message) => ({
                        ...message,
                        status: "error",
                        parts: [{ type: "notice", index: 0, text: detail, tone: "error" }],
                    })),
                );
                activeAssistantID.current = null;
                setProblem(detail);
                setRunning(false);
                return false;
            }
        },
        [running],
    );

    const stop = useCallback(() => {
        void ChatService.Stop();
    }, []);

    const clear = useCallback(() => {
        if (running) {
            return;
        }
        cancelFlush();
        buffer.current = null;
        activeAssistantID.current = null;
        setMessages([]);
        setProblem(null);
    }, [cancelFlush, running]);

    const reload = useCallback(() => {
        void ChatService.Reload()
            .then((next) => {
                applyStatus(next);
                setProblem(next.problem || null);
            })
            .catch((error: unknown) => setProblem(errorText(error)));
    }, [applyStatus]);

    return { status, messages, running, problem, send, stop, clear, reload };
}
