import fs from "fs";
import * as anchor from "@coral-xyz/anchor";
import "dotenv/config";
import { PublicKey, SystemProgram, Keypair, Connection } from "@solana/web3.js";
import { Pushsolanalocker } from "./type_pushsolanalocker";

(async () => {
  // 1. Load admin keypair explicitly
  const secret: number[] = JSON.parse(
    fs.readFileSync(`${process.env.HOME}/.config/solana/id.json`, "utf8")
  );
  const adminKeypair = Keypair.fromSecretKey(Uint8Array.from(secret));
  const admin: PublicKey = adminKeypair.publicKey;

  // 2. Create provider manually
  const connection = new Connection("http://127.0.0.1:8899", "confirmed");
  const wallet = new anchor.Wallet(adminKeypair);
  const provider = new anchor.AnchorProvider(connection, wallet, {
    commitment: "confirmed",
  });
  anchor.setProvider(provider);
  
  // 3. Load program
  const PROGRAM_ID = new PublicKey(process.env['SOLANA_PROGRAM_ID'] as string);
  console.log("hmml", PROGRAM_ID)
  const idl = JSON.parse(fs.readFileSync("pushsolanalocker.json", "utf8"));
  const program = new anchor.Program(idl as Pushsolanalocker, provider)
  console.log("Program loaded:", program.programId.toBase58());

  // 4. Derive PDAs
  const [lockerPda] = PublicKey.findProgramAddressSync(
    [Buffer.from("locker")],
    PROGRAM_ID
  );
  const [vaultPda] = PublicKey.findProgramAddressSync(
    [Buffer.from("vault")],
    PROGRAM_ID
  );

  // 5. Initialize if not already created
  console.log("2. Initializing locker...");
  const lockerAccount = await connection.getAccountInfo(lockerPda);
  console.log(lockerPda)
  console.log("localaccount", lockerAccount)

  const vaultbalance = await connection.getBalance(vaultPda);
  console.log("vaultbalance", vaultPda.toBase58())
  const lockerbalance = await connection.getBalance(lockerPda);
  console.log("lockerbalance : ", lockerbalance);
  

  if (!lockerAccount) {
    const tx = await program.methods
      .initialize()
      .accounts({
        locker: lockerPda, // <-- Make sure this matches your IDL account names
        vault: vaultPda,
        admin: admin,
        systemProgram: SystemProgram.programId,
      })
      .signers([adminKeypair]) // explicitly add signer
      .rpc();

    console.log(`✅ Locker initialized: ${tx}\n`);
  } else {
    console.log("✅ Locker already exists\n");
  }
})();
