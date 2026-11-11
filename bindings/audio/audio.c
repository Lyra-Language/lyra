// The C half of `bindings.audio`: one streaming decoder over four formats — MP3 (dr_mp3),
// FLAC (dr_flac), Ogg Vorbis (stb_vorbis) and WAV (dr_wav) — chosen by the file's first
// bytes. Each decodes from memory the caller keeps alive while it is open (the Lyra side
// holds the bytes in its `Decoder`), into 32-bit floats, channels interleaved.
//
// The decoders are vendored beside this file at the releases NOTICE.md names; each is
// public domain (or MIT-0), so whatever links them stays MIT.

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#define DR_MP3_IMPLEMENTATION
#define DR_MP3_NO_STDIO
#include "dr_mp3.h"
#define DR_FLAC_IMPLEMENTATION
#define DR_FLAC_NO_STDIO
#include "dr_flac.h"
#define DR_WAV_IMPLEMENTATION
#define DR_WAV_NO_STDIO
#include "dr_wav.h"
#define STB_VORBIS_NO_STDIO
#define STB_VORBIS_NO_PUSHDATA_API
#include "stb_vorbis.c"

enum { KIND_MP3 = 1, KIND_FLAC = 2, KIND_VORBIS = 3, KIND_WAV = 4 };

typedef struct lyra_audio {
    int kind;
    int channels;
    int rate;
    int64_t frames;
    drmp3 mp3;
    drwav wav;
    drflac *flac;
    stb_vorbis *vorbis;
} lyra_audio;

// The format the bytes' start says: 0 for none it reads.
static int kind_of(const uint8_t *b, size_t n) {
    if (n >= 4 && memcmp(b, "fLaC", 4) == 0) return KIND_FLAC;
    if (n >= 4 && memcmp(b, "OggS", 4) == 0) return KIND_VORBIS;
    if (n >= 12 && (memcmp(b, "RIFF", 4) == 0 || memcmp(b, "RF64", 4) == 0) && memcmp(b + 8, "WAVE", 4) == 0) return KIND_WAV;
    // An ID3 tag, or an MPEG frame's sync (11 bits set).
    if (n >= 3 && memcmp(b, "ID3", 3) == 0) return KIND_MP3;
    if (n >= 2 && b[0] == 0xFF && (b[1] & 0xE0) == 0xE0) return KIND_MP3;
    return 0;
}

// A decoder over `size` bytes at `data`, or NULL when they are no file of the four, or one
// damaged past reading. `data` must stay where it is until lyra_audio_close.
lyra_audio *lyra_audio_open(const uint8_t *data, int64_t size) {
    if (data == NULL || size <= 0) return NULL;
    lyra_audio *a = calloc(1, sizeof *a);
    if (a == NULL) return NULL;
    a->kind = kind_of(data, (size_t)size);
    switch (a->kind) {
    case KIND_MP3:
        if (!drmp3_init_memory(&a->mp3, data, (size_t)size, NULL)) break;
        a->channels = (int)a->mp3.channels;
        a->rate = (int)a->mp3.sampleRate;
        a->frames = (int64_t)drmp3_get_pcm_frame_count(&a->mp3);
        drmp3_seek_to_pcm_frame(&a->mp3, 0);
        return a;
    case KIND_FLAC:
        a->flac = drflac_open_memory(data, (size_t)size, NULL);
        if (a->flac == NULL) break;
        a->channels = (int)a->flac->channels;
        a->rate = (int)a->flac->sampleRate;
        a->frames = (int64_t)a->flac->totalPCMFrameCount;
        return a;
    case KIND_VORBIS: {
        int error = 0;
        a->vorbis = stb_vorbis_open_memory(data, (int)size, &error, NULL);
        if (a->vorbis == NULL) break;
        stb_vorbis_info info = stb_vorbis_get_info(a->vorbis);
        a->channels = info.channels;
        a->rate = (int)info.sample_rate;
        a->frames = (int64_t)stb_vorbis_stream_length_in_samples(a->vorbis);
        return a;
    }
    case KIND_WAV:
        if (!drwav_init_memory(&a->wav, data, (size_t)size, NULL)) break;
        a->channels = (int)a->wav.channels;
        a->rate = (int)a->wav.sampleRate;
        a->frames = (int64_t)a->wav.totalPCMFrameCount;
        return a;
    }
    free(a);
    return NULL;
}

int32_t lyra_audio_channels(const lyra_audio *a) { return a->channels; }
int32_t lyra_audio_rate(const lyra_audio *a) { return a->rate; }
int64_t lyra_audio_frames(const lyra_audio *a) { return a->frames; }

// Up to `frames` frames into `out` (frames × channels floats), from where it is: the
// frames read, fewer only at the end.
int64_t lyra_audio_read(lyra_audio *a, float *out, int64_t frames) {
    if (frames <= 0) return 0;
    switch (a->kind) {
    case KIND_MP3: return (int64_t)drmp3_read_pcm_frames_f32(&a->mp3, (drmp3_uint64)frames, out);
    case KIND_FLAC: return (int64_t)drflac_read_pcm_frames_f32(a->flac, (drflac_uint64)frames, out);
    case KIND_WAV: return (int64_t)drwav_read_pcm_frames_f32(&a->wav, (drwav_uint64)frames, out);
    case KIND_VORBIS: {
        int64_t got = 0;
        while (got < frames) {
            int want = (int)((frames - got) > 4096 ? 4096 : (frames - got));
            int n = stb_vorbis_get_samples_float_interleaved(a->vorbis, a->channels, out + got * a->channels, want * a->channels);
            if (n <= 0) break;
            got += n;
        }
        return got;
    }
    }
    return 0;
}

// Its place moved to frame `frame`: whether it could be.
int32_t lyra_audio_seek(lyra_audio *a, int64_t frame) {
    if (frame < 0) frame = 0;
    switch (a->kind) {
    case KIND_MP3: return drmp3_seek_to_pcm_frame(&a->mp3, (drmp3_uint64)frame) ? 1 : 0;
    case KIND_FLAC: return drflac_seek_to_pcm_frame(a->flac, (drflac_uint64)frame) ? 1 : 0;
    case KIND_WAV: return drwav_seek_to_pcm_frame(&a->wav, (drwav_uint64)frame) ? 1 : 0;
    case KIND_VORBIS: return stb_vorbis_seek(a->vorbis, (unsigned int)frame) ? 1 : 0;
    }
    return 0;
}

void lyra_audio_close(lyra_audio *a) {
    if (a == NULL) return;
    switch (a->kind) {
    case KIND_MP3: drmp3_uninit(&a->mp3); break;
    case KIND_FLAC: drflac_close(a->flac); break;
    case KIND_WAV: drwav_uninit(&a->wav); break;
    case KIND_VORBIS: stb_vorbis_close(a->vorbis); break;
    }
    free(a);
}
