export type MessageRole = "user" | "assistant";

export type MessageStatus = "pending" | "streaming" | "complete" | "error";

export type MessagePart =
    | {
          type: "text";
          index: number;
          text: string;
      }
    | {
          type: "reasoning";
          index: number;
          text: string;
      }
    | {
          type: "tool-call";
          index: number;
          name: string;
      }
    | {
          type: "notice";
          index: number;
          text: string;
          tone: "info" | "error";
      };

export interface ChatMessage {
    id: string;
    role: MessageRole;
    parts: MessagePart[];
    status: MessageStatus;
}

export function messageText(message: ChatMessage): string {
    return message.parts
        .filter((part): part is Extract<MessagePart, { type: "text" }> => part.type === "text")
        .map((part) => part.text)
        .join("");
}
