import { useCallback, useEffect, useState } from "react";
import { Window } from "@wailsio/runtime";

/**
 * WindowControls 是窗口自己画的最小化 / 最大化 / 关闭三个按钮。
 *
 * 窗口以 Frameless 启动，系统标题栏和它的按钮都不存在了，所以这三个动作
 * 必须由界面提供，否则关窗只剩 Alt+F4。
 *
 * 父元素给的是可拖拽区（--wails-draggable: drag），按钮本身继承全局的
 * no-drag，点下去不会变成拖动窗口。
 */
export function WindowControls() {
    const [maximised, setMaximised] = useState(false);

    // 最大化状态可能被窗口管理器（双击标题区、快捷键、平铺）改动，
    // 而不只是由下面的按钮触发，所以挂载时和每次窗口尺寸变化后都重新问一次。
    const syncMaximised = useCallback(() => {
        void Window.IsMaximised()
            .then(setMaximised)
            .catch(() => setMaximised(false));
    }, []);

    useEffect(() => {
        syncMaximised();

        window.addEventListener("resize", syncMaximised);

        return () => window.removeEventListener("resize", syncMaximised);
    }, [syncMaximised]);

    // 点击只负责发出请求，最终状态仍以 resize 回调读到的为准：
    // 这样按钮图标永远不会和窗口实际状态脱节。
    const toggleMaximise = useCallback(() => {
        void Window.ToggleMaximise().catch(() => undefined);
    }, []);

    const minimise = useCallback(() => {
        void Window.Minimise().catch(() => undefined);
    }, []);

    const close = useCallback(() => {
        void Window.Close().catch(() => undefined);
    }, []);

    return (
        <div className="window-controls">
            <button
                className="window-control"
                type="button"
                onClick={minimise}
                aria-label="最小化"
                title="最小化"
            >
                <svg viewBox="0 0 12 12" aria-hidden="true">
                    <path d="M2.5 6h7" />
                </svg>
            </button>

            <button
                className="window-control"
                type="button"
                onClick={toggleMaximise}
                aria-label={maximised ? "还原窗口" : "最大化"}
                title={maximised ? "还原窗口" : "最大化"}
            >
                {maximised ? (
                    <svg viewBox="0 0 12 12" aria-hidden="true">
                        <path d="M4.2 4.2V2.6h5.2v5.2H7.8" />
                        <rect x="2.6" y="4.4" width="5" height="5" rx="0.6" />
                    </svg>
                ) : (
                    <svg viewBox="0 0 12 12" aria-hidden="true">
                        <rect x="2.6" y="2.6" width="6.8" height="6.8" rx="0.6" />
                    </svg>
                )}
            </button>

            <button
                className="window-control is-close"
                type="button"
                onClick={close}
                aria-label="关闭"
                title="关闭"
            >
                <svg viewBox="0 0 12 12" aria-hidden="true">
                    <path d="m3.2 3.2 5.6 5.6M8.8 3.2 3.2 8.8" />
                </svg>
            </button>
        </div>
    );
}
