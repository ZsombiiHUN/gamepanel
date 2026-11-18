import { describe, expect, it } from "vitest";
import { sftpHost, sftpLink, urlHost } from "./sftp";

describe("sftpLink", () => {
  it("escapes the @ of the email in the username", () => {
    expect(sftpLink("node.example.com", 2022, "bob@example.com.abcdef12")).toBe("sftp://bob%40example.com.abcdef12@node.example.com:2022/");
  });
  it("leaves the default port out", () => {
    expect(sftpLink("h", 22, "u")).toBe("sftp://u@h/");
  });
  it("puts the password after the username, escaped", () => {
    expect(sftpLink("h", 2022, "a@b.c.12345678", "gp_x/y:z")).toBe("sftp://a%40b.c.12345678:gp_x%2Fy%3Az@h:2022/");
  });
  it("brackets IPv6 addresses", () => {
    expect(sftpLink("2001:db8::1", 2022, "u")).toBe("sftp://u@[2001:db8::1]:2022/");
    expect(urlHost("[::1]")).toBe("[::1]");
    expect(urlHost("10.0.0.1")).toBe("10.0.0.1");
  });
});

describe("sftpHost", () => {
  it("prefers the node's address", () => {
    expect(sftpHost("sftp.example.com", { hostname: "panel.example.com" })).toEqual({ host: "sftp.example.com", guessed: false });
  });
  it("falls back to the panel's host", () => {
    expect(sftpHost("", { hostname: "panel.example.com" })).toEqual({ host: "panel.example.com", guessed: true });
    expect(sftpHost("", { hostname: "[::1]" })).toEqual({ host: "::1", guessed: true });
  });
});
