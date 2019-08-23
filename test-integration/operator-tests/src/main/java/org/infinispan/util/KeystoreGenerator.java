package org.infinispan.util;

import java.io.FileWriter;
import java.io.IOException;
import java.io.Writer;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

import cz.xtf.core.config.OpenShiftConfig;
import lombok.Getter;

/**
 * Class for generating keystores signed by one common generated ca
 */
public class KeystoreGenerator {
	private static final Path TMP_DIRECTORY = Paths.get("tmp").toAbsolutePath();

	@Getter
	private static Path caDir;

	@Getter
	private static Path truststore;

	static {
		try {
			TMP_DIRECTORY.toFile().mkdirs();
			caDir = Files.createTempDirectory(TMP_DIRECTORY, "ca");

			// Generate key and cert
			processCall(caDir, "openssl", "req", "-new", "-newkey", "rsa:4096", "-x509", "-keyout", "ca-key.pem", "-out", "ca-certificate.pem", "-days", "365", "-passout", "pass:password", "-subj", "/C=CZ/ST=CZ/L=Brno/O=QE/CN=xtf.ca");

			// Generate truststore with ca cert
			processCall(caDir, "keytool", "-import", "-noprompt", "-keystore", "truststore", "-file", "ca-certificate.pem", "-alias", "xtf.ca", "-storepass", "password");

			// Import openshift server certs to truststore
			String server = OpenShiftConfig.url().replaceFirst("https://", "");
			server = server.contains(":") ? server : server + ":443";

			processCall(caDir, "/bin/sh", "-c", "echo \"Q\" | openssl s_client -connect " + server + " -showcerts 2>/dev/null > serversOpenSslResponse");
			String splitCmd = System.getProperty("os.name").toLowerCase().startsWith("mac") ? "gcsplit" : "csplit";
			processCall(caDir, "/bin/sh", "-c",  splitCmd + " -f serverCert -s serversOpenSslResponse '/^-----BEGIN CERTIFICATE-----$/' '{*}'");

			processCall(caDir, "/bin/sh", "-c", "find . -type f -not -name \"serverCert00\" -name \"serverCert[0-9][0-9]\" -exec openssl x509 -in {} -out {}.pem \\;");
			processCall(caDir, "/bin/sh", "-c", "find . -type f -name \"serverCert[0-9][0-9].pem\" -exec keytool -import -noprompt -keystore truststore -file {} -alias {} -storepass password \\;");

			truststore = caDir.resolve("truststore");
		} catch (IOException e) {
			throw new IllegalStateException("Failed to initialize ca", e);
		}
	}

	public static Path generateKeystore(String hostname, String[] alternativeHostnames) {
		return generateKeystore(hostname, alternativeHostnames, hostname, false);
	}

	public static Path generateKeystore(String hostname, String[] alternativeHostnames, String keyAlias, boolean deleteCaFromKeyStore) {
		String keystore = hostname + ".keystore";

		if (caDir.resolve(keystore).toFile().exists()) {
			return caDir.resolve(keystore);
		}

		processCall(caDir, "keytool", "-genkeypair", "-keyalg", "RSA", "-noprompt", "-alias", keyAlias, "-dname", "CN=" + hostname + ", OU=TF, O=XTF, L=Brno, S=CZ, C=CZ", "-keystore", keystore, "-storepass", "password", "-keypass", "password", "-deststoretype", "pkcs12");

		processCall(caDir, "keytool", "-keystore", keystore, "-certreq", "-alias", keyAlias, "--keyalg", "rsa", "-file", hostname + ".csr", "-storepass", "password");

		if (alternativeHostnames != null && alternativeHostnames.length > 0) {
			try {
				Writer writer = new FileWriter(caDir.resolve(keyAlias + ".extensions").toFile());
				writer.write("[ req_ext ]\n");
				writer.write("subjectAltName = @alt_names\n");
				writer.write("\n");
				writer.write("[ alt_names ]\n");

				writer.write("DNS.1 = " + hostname + "\n");
				for (int i = 0; i < alternativeHostnames.length; ++i) {
					writer.write("DNS." + (i + 2) + " = " + alternativeHostnames[i] + "\n");
				}
				writer.flush();
				writer.close();

				processCall(caDir, "openssl", "x509", "-req", "-CA", "ca-certificate.pem", "-CAkey", "ca-key.pem", "-in", hostname + ".csr", "-out", hostname + ".cer", "-days", "365", "-CAcreateserial", "-passin", "pass:password", "-extfile", keyAlias + ".extensions", "-extensions", "req_ext");

			} catch (IOException e) {
				throw new RuntimeException(e);
			}
		} else {
			processCall(caDir, "openssl", "x509", "-req", "-CA", "ca-certificate.pem", "-CAkey", "ca-key.pem", "-in", hostname + ".csr", "-out", hostname + ".cer", "-days", "365", "-CAcreateserial", "-passin", "pass:password");
		}

		processCall(caDir, "keytool", "-import", "-noprompt", "-keystore", keystore, "-file", "ca-certificate.pem", "-alias", "xtf.ca", "-storepass", "password");

		processCall(caDir, "keytool", "-import", "-keystore", keystore, "-file", hostname + ".cer", "-alias", keyAlias, "-storepass", "password");

		if (deleteCaFromKeyStore) {
			processCall(caDir, "keytool", "-delete", "-noprompt", "-alias", "xtf.ca", "-keystore", keystore, "-storepass", "password");
		}

		return caDir.resolve(keystore);
	}

	public static CertPaths generateCerts(String hostname) {
		return generateCerts(hostname, null);
	}

	public static CertPaths generateCerts(String hostname, String[] alternativeHostnames) {
		String keystore = hostname + ".keystore";

		generateKeystore(hostname, alternativeHostnames);

		// export cert as CN.keystore.pem
		processCall(caDir, "keytool", "-exportcert", "-rfc", "-keystore", keystore, "-alias", hostname, "-storepass", "password", "-file", keystore + ".pem");

		// convert to CN.keystore.keywithattrs.pem
		processCall(caDir, "openssl", "pkcs12", "-in", keystore, "-nodes", "-nocerts", "-out", keystore + ".keywithattrs.pem", "-passin", "pass:password");

		// Remove the Bag attributes to CN.keystore.key.pem
		processCall(caDir, "openssl", "rsa", "-in", keystore + ".keywithattrs.pem", "-out", keystore + ".key.pem");

		return new CertPaths(
				caDir.resolve("ca-certificate.pem"),
				caDir.resolve("truststore"),
				caDir.resolve(keystore),
				caDir.resolve(keystore + ".key.pem"),
				caDir.resolve(keystore + ".pem"));
	}

	private static void processCall(Path cwd, String... args) {
		ProcessBuilder pb = new ProcessBuilder(args);

		pb.directory(cwd.toFile());
		pb.redirectOutput(ProcessBuilder.Redirect.INHERIT);
		pb.redirectError(ProcessBuilder.Redirect.INHERIT);

		int result = -1;

		try {
			result = pb.start().waitFor();
		} catch (IOException | InterruptedException e) {
			throw new IllegalStateException("Failed executing " + String.join(" ", args));
		}

		if (result != 0) {
			throw new IllegalStateException("Failed executing " + String.join(" ", args));
		}
	}

	public static class CertPaths {

		public CertPaths(Path caPem, Path truststore, Path keystore, Path keyPem, Path certPem) {
			this.caPem = caPem;
			this.truststore = truststore;
			this.keystore = keystore;
			this.keyPem = keyPem;
			this.certPem = certPem;
		}

		public Path caPem;
		public Path truststore;
		public Path keystore;
		public Path keyPem;
		public Path certPem;
	}
}
