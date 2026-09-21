# Built by Copr from a source RPM the release workflow uploads. Copr builds
# without network access, so the Go dependencies come from a vendor tarball
# published alongside each release rather than being fetched here.
%{!?tsuzuki_version:%{error:define tsuzuki_version, e.g. rpmbuild --define 'tsuzuki_version 0.3.2' -bs packaging/tsuzuki.spec}}

# A static Go binary carries its own debug information and has no debug source
# to split out, so rpm's automatic debug packages find nothing to package.
%global debug_package %{nil}

Name:           tsuzuki
Version:        %{tsuzuki_version}
Release:        1%{?dist}
Summary:        Watch anime from your terminal in mpv, with AniList tracking

License:        MIT
URL:            https://github.com/EmoFa/tsuzuki
Source0:        %{url}/archive/v%{version}/%{name}-%{version}.tar.gz
Source1:        %{url}/releases/download/v%{version}/%{name}-%{version}-vendor.tar.gz

BuildRequires:  golang >= 1.25
Requires:       mpv
Recommends:     chromium

%description
tsuzuki finds anime episodes across several providers and plays them in mpv,
resuming where you left off. It tracks progress locally or on AniList, skips
openings, endings and filler episodes, and shows what you're watching on
Discord.

%prep
%autosetup -n %{name}-%{version} -a 1

%build
export CGO_ENABLED=0
export GOFLAGS="-mod=vendor -trimpath"
go build -ldflags "-X github.com/EmoFa/tsuzuki/internal/buildinfo.Version=%{version}" \
    -o %{name} ./cmd/%{name}

%install
install -Dpm 0755 %{name} %{buildroot}%{_bindir}/%{name}
install -Dpm 0644 docs/config.md %{buildroot}%{_docdir}/%{name}/config.md

%check
# Tests needing mpv, Chromium or a provider skip themselves.
go test ./...

%files
%license LICENSE
%doc README.md
%{_bindir}/%{name}
%{_docdir}/%{name}/config.md

%changelog
* Sun Sep 20 2026 EmoFa <https://github.com/EmoFa> - %{tsuzuki_version}-1
- See https://github.com/EmoFa/tsuzuki/releases
