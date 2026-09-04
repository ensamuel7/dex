// === Public-key signature verification ===
//
// Everything above this point is symmetric: a digest, or a MAC computed with a
// secret both sides already hold. That is enough to check a webhook from a payment
// provider and not enough to check anything an identity provider signs. Google and
// Apple sign ID tokens with RSA and publish only the public half, as a JWK — a
// modulus and an exponent, base64url — so a caller has no shared secret to compare
// against and no PEM to load either.
//
// The two functions here are what that leaves missing: the alphabet JWTs are
// actually written in, and the verification itself.

// Base64url, per RFC 4648 §5: '-' and '_' where ordinary base64 has '+' and '/'.
//
// A separate entry point rather than a flag on base64Decode, because that one
// skips characters it does not recognise — the right call for a PEM body with
// newlines in it, and silently wrong for a JWT, where '-' and '_' would be
// dropped and the bytes would come back the wrong length rather than refused.
//
// Standard base64 is accepted too. The two alphabets disagree only on those two
// characters, so taking both costs nothing and spares every caller a decision
// about which one it has.
static int dex_b64url_value(int c) {
    if (c == '-') return 62;
    if (c == '_') return 63;
    return dex_b64_value(c);
}

DexString* dex_crypto_base64url_decode(DexString* in) {
    if (!in) return dex_string_new("", 0);
    size_t n = in->len;
    /* Upper bound: every 4 input chars yield at most 3 bytes. Padding is
       optional in base64url and usually absent, which this handles by
       accumulating bits rather than stepping in quads. */
    size_t cap = (n / 4 + 1) * 3 + 3;
    DexString* out = (DexString*)dex_obj_alloc(sizeof(DexString) + cap + 1, dex_string_destroy);
    unsigned char* d = (unsigned char*)out->data;

    unsigned acc = 0;
    int bits = 0;
    size_t produced = 0;
    for (size_t i = 0; i < n; i++) {
        int v = dex_b64url_value((unsigned char)in->data[i]);
        if (v < 0) continue; /* '=' padding, and any newline in a wrapped key */
        acc = (acc << 6) | (unsigned)v;
        bits += 6;
        if (bits >= 8) {
            bits -= 8;
            d[produced++] = (unsigned char)((acc >> bits) & 0xFF);
        }
    }
    out->len = produced;
    out->data[produced] = '\0';
    return out;
}

#ifdef DEX_HAS_SSL
#include <openssl/bn.h>
#include <openssl/rsa.h>
#if OPENSSL_VERSION_NUMBER >= 0x30000000L
#include <openssl/core_names.h>
#include <openssl/param_build.h>
#endif

/* An RSA public key assembled from the two numbers a JWK carries. There is no
   PEM in a JWKS document and no way to build one without writing DER by hand,
   so the key is constructed from its parts instead.

   OpenSSL 3 deprecated the RSA_* accessors, so the two eras take different
   routes to the same key rather than the older one compiling with warnings
   that a -Werror build would reject. */
static EVP_PKEY* dex_rsa_public_from(DexString* modulus, DexString* exponent) {
    if (!modulus || !exponent || modulus->len == 0 || exponent->len == 0) {
        return NULL;
    }
    BIGNUM* n = BN_bin2bn((const unsigned char*)modulus->data, (int)modulus->len, NULL);
    BIGNUM* e = BN_bin2bn((const unsigned char*)exponent->data, (int)exponent->len, NULL);
    if (!n || !e) {
        BN_free(n);
        BN_free(e);
        return NULL;
    }

    EVP_PKEY* key = NULL;
#if OPENSSL_VERSION_NUMBER >= 0x30000000L
    OSSL_PARAM_BLD* bld = OSSL_PARAM_BLD_new();
    OSSL_PARAM* params = NULL;
    EVP_PKEY_CTX* ctx = NULL;
    if (bld
        && OSSL_PARAM_BLD_push_BN(bld, OSSL_PKEY_PARAM_RSA_N, n)
        && OSSL_PARAM_BLD_push_BN(bld, OSSL_PKEY_PARAM_RSA_E, e)
        && (params = OSSL_PARAM_BLD_to_param(bld)) != NULL
        && (ctx = EVP_PKEY_CTX_new_from_name(NULL, "RSA", NULL)) != NULL
        && EVP_PKEY_fromdata_init(ctx) > 0) {
        if (EVP_PKEY_fromdata(ctx, &key, EVP_PKEY_PUBLIC_KEY, params) <= 0) {
            key = NULL;
        }
    }
    EVP_PKEY_CTX_free(ctx);
    OSSL_PARAM_free(params);
    OSSL_PARAM_BLD_free(bld);
    /* fromdata copies, so both numbers are ours to free either way. */
    BN_free(n);
    BN_free(e);
#else
    RSA* rsa = RSA_new();
    if (rsa && RSA_set0_key(rsa, n, e, NULL)) {
        /* rsa owns n and e from here, including on the failure paths below. */
        key = EVP_PKEY_new();
        if (key && EVP_PKEY_assign_RSA(key, rsa) <= 0) {
            EVP_PKEY_free(key);
            RSA_free(rsa);
            key = NULL;
        } else if (!key) {
            RSA_free(rsa);
        }
    } else {
        BN_free(n);
        BN_free(e);
        if (rsa) RSA_free(rsa);
    }
#endif
    return key;
}

_Bool dex_crypto_verify_rs256(DexString* message, DexString* signature,
                              DexString* modulus, DexString* exponent) {
    /* An empty signature is not a signature. Without this the verify below
       would be asked to check nothing against nothing. */
    if (!signature || signature->len == 0) {
        return 0;
    }
    EVP_PKEY* key = dex_rsa_public_from(modulus, exponent);
    if (!key) {
        return 0;
    }

    EVP_MD_CTX* ctx = EVP_MD_CTX_new();
    int ok = 0;
    if (ctx && EVP_DigestVerifyInit(ctx, NULL, EVP_sha256(), NULL, key) == 1) {
        const unsigned char* m = message ? (const unsigned char*)message->data
                                         : (const unsigned char*)"";
        size_t mlen = message ? message->len : 0;
        /* One-shot: the signed input is a JWT's header.payload, which is
           already in memory whole. EVP_DigestVerify is constant-time in the
           only sense that matters here — it returns a verdict, never a
           position at which two things stopped agreeing. */
        ok = EVP_DigestVerify(ctx, (const unsigned char*)signature->data, signature->len,
                              m, mlen) == 1;
    }
    EVP_MD_CTX_free(ctx);
    EVP_PKEY_free(key);
    return ok ? 1 : 0;
}

#else

/* Built without OpenSSL. Refusing every signature is the only safe answer: a
   verifier that cannot verify must not report success, and a caller that treats
   this as "signature checked" would be admitting anyone. Same contract as the
   digests above, which return an empty string rather than a wrong one. */
_Bool dex_crypto_verify_rs256(DexString* message, DexString* signature,
                              DexString* modulus, DexString* exponent) {
    (void)message; (void)signature; (void)modulus; (void)exponent;
    fprintf(stderr, "[crypto] verifyRs256 needs OpenSSL; this build has none\n");
    return 0;
}

#endif
