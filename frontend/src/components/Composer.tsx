import { useCallback, useMemo, type KeyboardEvent } from "react";
import CodeMirror, { EditorView } from "@uiw/react-codemirror";
import { markdown } from "@codemirror/lang-markdown";

interface ComposerProps {
    value: string;
    onChange: (value: string) => void;
    onSubmit: () => void;
    onStop: () => void;
    running: boolean;
    disabled?: boolean;
}

function SendIcon() {
    return (
        <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="m5 12 14-7-4.7 14-2.5-5.1L5 12Z" />
            <path d="m11.8 13.9 3.4-3.4" />
        </svg>
    );
}

function StopIcon() {
    return (
        <svg viewBox="0 0 24 24" aria-hidden="true">
            <rect x="7" y="7" width="10" height="10" rx="2" />
        </svg>
    );
}

export function Composer({
    value,
    onChange,
    onSubmit,
    onStop,
    running,
    disabled = false,
}: ComposerProps) {
    // 可编辑区需要自己的无障碍名称：aria-label 写在 <CodeMirror> 上只会
    // 落到外层容器 div，读屏软件找不到真正的输入区域。
    // lineWrapping 关掉 CodeMirror 默认的横向滚动：聊天输入框应当折行。
    const extensions = useMemo(
        () => [
            markdown(),
            EditorView.lineWrapping,
            EditorView.contentAttributes.of({ "aria-label": "消息输入", "aria-multiline": "true" }),
        ],
        [],
    );

    const submit = useCallback(() => {
        if (!disabled && !running && value.trim() !== "") {
            onSubmit();
        }
    }, [disabled, onSubmit, running, value]);

    const handleKeyDown = useCallback(
        (event: KeyboardEvent<HTMLDivElement>) => {
            if (event.key !== "Enter" || event.shiftKey) {
                return;
            }

            // 中文输入法确认候选词时也会发 Enter，且部分输入法下
            // isComposing 并不可靠，keyCode 229 是同一件事的另一条线索。
            if (event.nativeEvent.isComposing || event.keyCode === 229) {
                return;
            }

            event.preventDefault();
            submit();
        },
        [submit],
    );

    return (
        <div className="composer-wrap">
            <div className={`composer${disabled ? " is-disabled" : ""}`} onKeyDownCapture={handleKeyDown}>
                <CodeMirror
                    value={value}
                    onChange={onChange}
                    extensions={extensions}
                    editable={!disabled}
                    placeholder={disabled ? "请先配置模型连接" : "询问、构思，或粘贴 Markdown…"}
                    minHeight="68px"
                    maxHeight="180px"
                    theme="light"
                    basicSetup={{
                        lineNumbers: false,
                        foldGutter: false,
                        highlightActiveLine: false,
                        highlightActiveLineGutter: false,
                        allowMultipleSelections: false,
                    }}
                />

                <div className="composer-toolbar">
                    <span className="composer-hint">
                        {running ? "正在生成，可以先写下一句" : "Enter 发送 · Shift + Enter 换行"}
                    </span>
                    {running ? (
                        <button className="composer-action is-stop" type="button" onClick={onStop}>
                            <StopIcon />
                            <span>停止</span>
                        </button>
                    ) : (
                        <button
                            className="composer-action"
                            type="button"
                            onClick={submit}
                            disabled={disabled || value.trim() === ""}
                        >
                            <SendIcon />
                            <span>发送</span>
                        </button>
                    )}
                </div>
            </div>
            <p className="composer-footnote">模型可能会出错，请检查重要信息。</p>
        </div>
    );
}
